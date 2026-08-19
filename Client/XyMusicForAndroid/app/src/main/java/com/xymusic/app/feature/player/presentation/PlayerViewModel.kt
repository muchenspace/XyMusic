package com.xymusic.app.feature.player.presentation

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.xymusic.app.R
import com.xymusic.app.core.common.DefaultDispatcher
import com.xymusic.app.core.common.runCatchingPreservingCancellation
import com.xymusic.app.core.model.media.Lyrics
import com.xymusic.app.core.model.media.LyricsFormat
import com.xymusic.app.core.model.media.LyricsTiming
import com.xymusic.app.feature.player.domain.LyricsSource
import com.xymusic.app.feature.player.domain.PlaybackQueueUseCases
import com.xymusic.app.feature.player.domain.PlayerEvent
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.PlayerUseCases
import com.xymusic.app.feature.player.domain.model.PlayerFailure
import com.xymusic.app.feature.player.domain.model.PlayerState
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
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext

@HiltViewModel
@OptIn(ExperimentalCoroutinesApi::class)
class PlayerViewModel
@Inject
constructor(
    private val playerUseCases: PlayerUseCases,
    private val lyricsSource: LyricsSource,
    private val playbackQueueUseCases: PlaybackQueueUseCases,
    @DefaultDispatcher private val defaultDispatcher: CoroutineDispatcher,
) : ViewModel() {
    private val mutableEffects = MutableSharedFlow<PlayerUiEffect>(extraBufferCapacity = 1)
    private val lyricsRefreshMutex = Mutex()
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
                                    refreshLyricsForTrack(trackId)
                                }
                            }
                            this@flow.emitAll(
                                lyricsSource.observe(trackId),
                            )
                        }
                    }
                }
            }

    private val parsedLyrics =
        selectedLyrics
            .map { lyrics ->
                lyrics?.let {
                    SelectedLyricsContent(
                        trackId = it.trackId,
                        language = it.language,
                        format = it.format,
                        timing = it.timing,
                        content = it.content,
                    )
                }
            }.distinctUntilChanged()
            .mapLatest { lyrics ->
                withContext(defaultDispatcher) {
                    lyrics?.let {
                        ParsedLyricsForTrack(
                            trackId = it.trackId,
                            lyrics = runCatchingPreservingCancellation {
                                parsePlayerLyrics(
                                    content = it.content,
                                    format = it.format,
                                    timing = it.timing,
                                    language = it.language,
                                )
                            }.getOrDefault(ParsedPlayerLyrics.Empty),
                        )
                    } ?: ParsedLyricsForTrack.Empty
                }
            }
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000),
                initialValue = ParsedLyricsForTrack.Empty,
            )

    private val structuralPlayerState =
        playerUseCases.state
            .distinctUntilChanged { previous, current ->
                previous.isStructurallyEqualTo(current)
            }.map(PlayerState::withoutPlaybackPosition)

    /**
     * State consumed by the composed player tree. It is built from the
     * position-free player projection, so position samples never rebuild the
     * lyrics/UI state pipeline or compare the complete PlayerUiState.
     */
    val structuralUiState =
        combine(
            structuralPlayerState,
            parsedLyrics,
        ) { player, lyrics ->
            buildPlayerUiState(player, lyrics)
        }
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000),
                initialValue = buildPlayerUiState(
                    playerUseCases.state.value.withoutPlaybackPosition(),
                    parsedLyrics.value,
                ),
            )

    /** Backward-compatible public state alias without high-frequency playback samples. */
    val uiState = structuralUiState

    /** The source for the single frame-clock playback position projection. */
    val playbackState = playerUseCases.state

    init {
        viewModelScope.launch {
            if (playbackQueueUseCases.observe().first().isNotEmpty()) playerUseCases.connect()
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
            if (playerUseCases.state.value.isPlaying) playerUseCases.pause() else playerUseCases.play()
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
        val nextMode = playerUseCases.state.value.nextPlaybackMode()
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
        val queue = playerUseCases.state.value.queue
        val currentIndex = queue.indexOfFirst { it.queueItemId == queueItemId }
        val targetIndex = currentIndex + direction
        if (currentIndex < 0 || targetIndex !in queue.indices) return
        execute { playerUseCases.moveQueueItem(queueItemId, targetIndex) }
    }

    fun clearQueue() {
        execute { playerUseCases.clearQueue() }
    }

    fun refreshLyrics() {
        val trackId = playerUseCases.state.value.currentItem?.trackId ?: return
        viewModelScope.launch {
            runCatchingPreservingCancellation {
                refreshLyricsForTrack(trackId)
            }
        }
    }

    private suspend fun refreshLyricsForTrack(trackId: String) {
        lyricsRefreshMutex.withLock {
            lyricsSource.refresh(trackId)
        }
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

private fun buildPlayerUiState(player: PlayerState, parsedLyrics: ParsedLyricsForTrack): PlayerUiState {
    val lyrics =
        parsedLyrics.lyrics.takeIf {
            parsedLyrics.trackId != null && parsedLyrics.trackId == player.currentItem?.trackId
        }
            ?: ParsedPlayerLyrics.Empty
    return PlayerUiState(
        player = player,
        lyrics = lyrics.lines,
        lyricsLanguage = lyrics.language,
        synchronizedLyrics = lyrics.synchronized,
        lyricsTiming = lyrics.timing,
        sleepTimerRemainingMs = player.sleepTimerRemainingMs,
    )
}

private fun PlayerFailure.messageRes(): Int = when (this) {
    PlayerFailure.ConnectionUnavailable -> R.string.player_connection_unavailable
    PlayerFailure.InvalidQueue -> R.string.player_invalid_queue
    PlayerFailure.PlaybackUnavailable -> R.string.player_playback_unavailable
    is PlayerFailure.Unexpected -> R.string.player_playback_failed
}

internal const val PLAYER_FAILURE_MESSAGE_DELAY_MS = 300L

private data class SelectedLyricsContent(
    val trackId: String,
    val language: String,
    val format: LyricsFormat,
    val timing: LyricsTiming,
    val content: String,
)

private data class ParsedLyricsForTrack(val trackId: String?, val lyrics: ParsedPlayerLyrics) {
    companion object {
        val Empty = ParsedLyricsForTrack(trackId = null, lyrics = ParsedPlayerLyrics.Empty)
    }
}
