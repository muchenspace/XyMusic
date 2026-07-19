package com.xymusic.app.feature.player.presentation

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.xymusic.app.R
import com.xymusic.app.core.common.DefaultDispatcher
import com.xymusic.app.core.common.runCatchingPreservingCancellation
import com.xymusic.app.core.model.media.Lyrics
import com.xymusic.app.core.model.media.LyricsFormat
import com.xymusic.app.core.preferences.AppSettingsRepository
import com.xymusic.app.feature.player.domain.LyricsSource
import com.xymusic.app.feature.player.domain.PlaybackQueueStore
import com.xymusic.app.feature.player.domain.PlayerEvent
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.PlayerUseCases
import com.xymusic.app.feature.player.domain.model.PlayerFailure
import com.xymusic.app.feature.player.domain.model.RepeatMode
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.emitAll
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.mapLatest
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

@HiltViewModel
@OptIn(ExperimentalCoroutinesApi::class)
class PlayerViewModel
@Inject
constructor(
    private val playerUseCases: PlayerUseCases,
    private val lyricsSource: LyricsSource,
    private val playbackQueueStore: PlaybackQueueStore,
    private val appSettingsRepository: AppSettingsRepository,
    @DefaultDispatcher private val defaultDispatcher: CoroutineDispatcher,
) : ViewModel() {
    private val mutableEffects = MutableSharedFlow<PlayerUiEffect>(extraBufferCapacity = 1)
    val effects = mutableEffects.asSharedFlow()

    private val selectedLyrics =
        playerUseCases.state
            .map { state -> state.currentItem?.trackId }
            .distinctUntilChanged()
            .flatMapLatest { trackId ->
                if (trackId == null) {
                    flowOf(null)
                } else {
                    flow<Lyrics?> {
                        coroutineScope {
                            launch {
                                runCatchingPreservingCancellation {
                                    lyricsSource.refresh(trackId)
                                }
                            }
                            this@flow.emitAll(
                                lyricsSource.observe(trackId).map { lyrics ->
                                    lyrics.firstOrNull(Lyrics::isDefault)
                                        ?: lyrics.firstOrNull()
                                },
                            )
                        }
                    }
                }
            }

    private val parsedLyrics =
        selectedLyrics
            .map { lyrics ->
                lyrics?.let {
                    SelectedLyricsContent(it.language, it.format, it.content)
                }
            }.distinctUntilChanged()
            .mapLatest { lyrics ->
                withContext(defaultDispatcher) {
                    lyrics?.let {
                        parsePlayerLyrics(
                            content = it.content,
                            format = it.format,
                            language = it.language,
                        )
                    } ?: ParsedPlayerLyrics.Empty
                }
            }

    val uiState =
        combine(
            playerUseCases.state,
            parsedLyrics,
            appSettingsRepository.settings,
        ) { player, lyrics, settings ->
            PlayerUiState(
                player = player,
                lyrics = lyrics.lines,
                lyricsLanguage = lyrics.language,
                synchronizedLyrics = lyrics.synchronized,
                wordByWordLyricsEnabled = settings.wordByWordLyricsEnabled,
                currentLyricIndex = lyrics.currentLineIndex(player.positionMs),
                sleepTimerRemainingMs = player.sleepTimerRemainingMs,
            )
        }.stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000),
            initialValue = PlayerUiState(),
        )

    init {
        viewModelScope.launch {
            if (playbackQueueStore.observe().first().isNotEmpty()) playerUseCases.connect()
        }
        viewModelScope.launch {
            playerUseCases.state
                .map { state -> state.failure }
                .distinctUntilChanged()
                .mapLatest { failure ->
                    if (failure != null) delay(PLAYER_FAILURE_MESSAGE_DELAY_MS)
                    failure
                }
                .filterNotNull()
                .collect { failure ->
                    mutableEffects.emit(PlayerUiEffect.ShowMessage(failure.messageRes()))
                }
        }
        viewModelScope.launch {
            playerUseCases.events.collect { event ->
                when (event) {
                    PlayerEvent.CompatibleCodecFallbackApplied ->
                        mutableEffects.emit(
                            PlayerUiEffect.ShowMessage(R.string.player_codec_fallback_applied),
                        )
                }
            }
        }
    }

    fun togglePlayback() {
        execute {
            if (uiState.value.player.isPlaying) playerUseCases.pause() else playerUseCases.play()
        }
    }

    fun seekTo(positionMs: Long) {
        execute { playerUseCases.seekTo(positionMs.coerceAtLeast(0)) }
    }

    fun skipToPrevious() {
        execute { playerUseCases.skipToPrevious() }
    }

    fun skipToNext() {
        execute { playerUseCases.skipToNext() }
    }

    fun cyclePlaybackMode() {
        val nextMode = uiState.value.player.nextPlaybackMode()
        execute { setPlaybackMode(nextMode) }
    }

    fun setPlaybackSpeed(speed: Float) {
        execute { playerUseCases.setPlaybackSpeed(speed) }
    }

    fun setSleepTimer(minutes: Int?) {
        val durationMs =
            minutes?.let {
                require(it > 0) { "Sleep timer duration must be positive" }
                it * 60_000L
            }
        execute { playerUseCases.setSleepTimer(durationMs) }
    }

    fun selectQueueItem(queueItemId: String) {
        execute { playerUseCases.seekToQueueItem(queueItemId) }
    }

    fun removeQueueItem(queueItemId: String) {
        execute { playerUseCases.removeFromQueue(queueItemId) }
    }

    fun moveQueueItem(queueItemId: String, direction: Int) {
        val queue = uiState.value.player.queue
        val currentIndex = queue.indexOfFirst { it.queueItemId == queueItemId }
        val targetIndex = currentIndex + direction
        if (currentIndex < 0 || targetIndex !in queue.indices) return
        execute { playerUseCases.moveQueueItem(queueItemId, targetIndex) }
    }

    fun clearQueue() {
        execute { playerUseCases.clearQueue() }
    }

    private suspend fun setPlaybackMode(mode: PlayerPlaybackMode): PlayerResult<Unit> {
        val repeatResult =
            playerUseCases.setRepeatMode(
                when (mode) {
                    PlayerPlaybackMode.Shuffle,
                    PlayerPlaybackMode.RepeatAll,
                    -> RepeatMode.ALL
                    PlayerPlaybackMode.RepeatOne -> RepeatMode.ONE
                },
            )
        if (repeatResult is PlayerResult.Failure) return repeatResult
        return playerUseCases.setShuffleEnabled(mode == PlayerPlaybackMode.Shuffle)
    }

    private fun execute(showFailure: Boolean = true, command: suspend () -> PlayerResult<Unit>) {
        viewModelScope.launch {
            val failed =
                runCatchingPreservingCancellation {
                    command()
                }.getOrNull() is PlayerResult.Failure
            if (failed && showFailure) {
                mutableEffects.emit(PlayerUiEffect.ShowMessage(R.string.player_command_failed))
            }
        }
    }
}

private fun PlayerFailure.messageRes(): Int = when (this) {
    PlayerFailure.ConnectionUnavailable -> R.string.player_connection_unavailable
    PlayerFailure.InvalidQueue -> R.string.player_invalid_queue
    PlayerFailure.PlaybackUnavailable -> R.string.player_playback_unavailable
    is PlayerFailure.Unexpected -> R.string.player_playback_failed
}

internal const val PLAYER_FAILURE_MESSAGE_DELAY_MS = 300L

private data class SelectedLyricsContent(val language: String, val format: LyricsFormat, val content: String)
