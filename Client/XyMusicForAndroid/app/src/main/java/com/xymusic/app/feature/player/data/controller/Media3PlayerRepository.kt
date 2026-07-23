package com.xymusic.app.feature.player.data.controller

import android.net.Uri
import android.os.Bundle
import android.os.SystemClock
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.Player
import androidx.media3.session.MediaController
import androidx.media3.session.SessionCommand
import androidx.media3.session.SessionResult
import com.xymusic.app.core.common.DefaultDispatcher
import com.xymusic.app.feature.player.data.media.PlaybackMediaMetadata
import com.xymusic.app.feature.player.data.media.PlaybackMediaUri
import com.xymusic.app.feature.player.domain.PlayerEvent
import com.xymusic.app.feature.player.domain.PlayerRepository
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.model.PlaybackState
import com.xymusic.app.feature.player.domain.model.PlayerConnectionState
import com.xymusic.app.feature.player.domain.model.PlayerFailure
import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
import com.xymusic.app.feature.player.domain.model.PlayerState
import com.xymusic.app.feature.player.domain.model.RepeatMode
import com.xymusic.app.feature.player.service.MAX_SLEEP_TIMER_DURATION_MS
import com.xymusic.app.feature.player.service.PlaybackSessionCommands
import java.util.UUID
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

@Singleton
class Media3PlayerRepository
@Inject
constructor(
    private val connection: MediaControllerConnection,
    @DefaultDispatcher private val defaultDispatcher: CoroutineDispatcher,
) : PlayerRepository {
    private val mutableState = MutableStateFlow(PlayerState())
    override val state: StateFlow<PlayerState> = mutableState.asStateFlow()
    private val mutableEvents = MutableSharedFlow<PlayerEvent>(extraBufferCapacity = 8)
    override val events = mutableEvents.asSharedFlow()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var attachedController: MediaController? = null
    private var positionUpdateJob: Job? = null
    private var queueMappingJob: Job? = null
    private var sleepTimerUpdateJob: Job? = null
    private var queueMappingGeneration = 0L
    private var sleepTimerSyncGeneration = 0L
    private var sleepTimerDeadlineElapsedRealtimeMs: Long? = null

    private val connectionListener =
        object : MediaControllerConnection.Listener {
            override fun onDisconnected(controller: MediaController) {
                onControllerDisconnected(controller)
            }

            override fun onCustomCommand(controller: MediaController, command: SessionCommand, args: Bundle): Boolean {
                if (controller !== attachedController) return false
                return when (command.customAction) {
                    PlaybackSessionCommands.SLEEP_TIMER_CHANGED.customAction -> {
                        updateSleepTimerDeadline(PlaybackSessionCommands.sleepTimerDeadline(args))
                        true
                    }
                    else -> {
                        val event = playerEventForCustomAction(command.customAction) ?: return false
                        mutableEvents.tryEmit(event)
                        true
                    }
                }
            }
        }

    private val listener =
        object : Player.Listener {
            override fun onEvents(player: Player, events: Player.Events) {
                if (player !== attachedController) return
                updateState(
                    player = player,
                    rebuildQueue =
                    events.contains(Player.EVENT_TIMELINE_CHANGED) ||
                        events.contains(Player.EVENT_MEDIA_METADATA_CHANGED),
                    positionDiscontinuity =
                    events.contains(Player.EVENT_POSITION_DISCONTINUITY) ||
                        events.contains(Player.EVENT_MEDIA_ITEM_TRANSITION),
                )
            }
        }

    init {
        connection.addListener(connectionListener)
    }

    override suspend fun connect(): PlayerResult<Unit> = withContext(Dispatchers.Main.immediate) {
        mutableState.value =
            mutableState.value.copy(
                connectionState = PlayerConnectionState.CONNECTING,
                failure = null,
            )
        try {
            attach(connection.connect())
            PlayerResult.Success(Unit)
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: Exception) {
            detachController()
            mutableState.value = disconnectedPlayerState()
            PlayerResult.Failure(PlayerFailure.ConnectionUnavailable)
        }
    }

    override suspend fun disconnect() = withContext(Dispatchers.Main.immediate) {
        detachController()
        connection.disconnect()
        mutableState.value = PlayerState()
    }

    override suspend fun setQueue(
        items: List<PlayerQueueItem>,
        startQueueItemId: String?,
        startPositionMs: Long,
        playWhenReady: Boolean,
    ): PlayerResult<Unit> {
        val mediaItems =
            withContext(defaultDispatcher) {
                if (isValidPlayerQueue(items, startQueueItemId, startPositionMs)) {
                    items.map(::toMediaItem)
                } else {
                    null
                }
            } ?: return PlayerResult.Failure(PlayerFailure.InvalidQueue)
        return withController(
            rebuildQueueAfterCommand = true,
            positionDiscontinuity = { true },
        ) { controller ->
            if (mediaItems.isEmpty()) {
                controller.clearMediaItems()
            } else {
                val startIndex =
                    startQueueItemId?.let { id ->
                        items.indexOfFirst { it.queueItemId == id }
                    } ?: 0
                controller.setMediaItems(mediaItems, startIndex, startPositionMs)
                controller.prepare()
                controller.playWhenReady = playWhenReady
            }
        }
    }

    override suspend fun addToQueue(items: List<PlayerQueueItem>): PlayerResult<Unit> {
        val mediaItems =
            withContext(defaultDispatcher) {
                if (items.isNotEmpty() && isValidPlayerQueue(items, null, 0)) {
                    items.map(::toMediaItem)
                } else {
                    null
                }
            } ?: return PlayerResult.Failure(PlayerFailure.InvalidQueue)
        return withController(rebuildQueueAfterCommand = true) { controller ->
            val existingQueueItemIds =
                (0 until controller.mediaItemCount)
                    .mapTo(mutableSetOf()) { index -> controller.getMediaItemAt(index).mediaId }
            if (mediaItems.any { it.mediaId in existingQueueItemIds }) {
                return@withController invalidQueue()
            }
            controller.addMediaItems(mediaItems)
        }
    }

    override suspend fun removeFromQueue(queueItemId: String): PlayerResult<Unit> =
        withController(rebuildQueueAfterCommand = true) {
            val index = indexOf(it, queueItemId)
            if (index < 0) return@withController invalidQueue()
            it.removeMediaItem(index)
            PlayerResult.Success(Unit)
        }

    override suspend fun moveQueueItem(queueItemId: String, newIndex: Int): PlayerResult<Unit> =
        withController(rebuildQueueAfterCommand = true) {
            val oldIndex = indexOf(it, queueItemId)
            if (oldIndex < 0 || newIndex !in 0 until it.mediaItemCount) {
                return@withController invalidQueue()
            }
            it.moveMediaItem(oldIndex, newIndex)
            PlayerResult.Success(Unit)
        }

    override suspend fun clearQueue(): PlayerResult<Unit> =
        withController(rebuildQueueAfterCommand = true) { it.clearMediaItems() }

    override suspend fun play(): PlayerResult<Unit> = withController {
        if (it.playbackState == Player.STATE_IDLE && it.currentMediaItem != null) it.prepare()
        it.play()
    }

    override suspend fun pause(): PlayerResult<Unit> = withController { it.pause() }

    override suspend fun seekTo(positionMs: Long): PlayerResult<Unit> {
        if (positionMs < 0) return invalidQueue()
        var didChangePosition = false
        return withController(positionDiscontinuity = { didChangePosition }) { controller ->
            didChangePosition = controller.currentPosition.coerceAtLeast(0) != positionMs
            controller.seekTo(positionMs)
        }
    }

    override suspend fun seekToQueueItem(queueItemId: String, positionMs: Long): PlayerResult<Unit> {
        if (positionMs < 0) return invalidQueue()
        var didChangePosition = false
        return withController(positionDiscontinuity = { didChangePosition }) { controller ->
            val index = indexOf(controller, queueItemId)
            if (index < 0) return@withController invalidQueue()
            didChangePosition =
                index != controller.currentMediaItemIndex ||
                    controller.currentPosition.coerceAtLeast(0) != positionMs
            controller.seekTo(index, positionMs)
            PlayerResult.Success(Unit)
        }
    }

    override suspend fun skipToNext(): PlayerResult<Unit> {
        var didChangePosition = false
        return withController(positionDiscontinuity = { didChangePosition }) { controller ->
            if (controller.hasNextMediaItem()) {
                didChangePosition = true
                controller.seekToNextMediaItem()
            }
        }
    }

    override suspend fun skipToPrevious(): PlayerResult<Unit> {
        var didChangePosition = false
        return withController(positionDiscontinuity = { didChangePosition }) { controller ->
            if (controller.hasPreviousMediaItem()) {
                didChangePosition = true
                controller.seekToPreviousMediaItem()
            } else {
                didChangePosition = controller.currentPosition.coerceAtLeast(0) != 0L
                controller.seekTo(0)
            }
        }
    }

    override suspend fun setRepeatMode(mode: RepeatMode): PlayerResult<Unit> = withController {
        it.repeatMode =
            when (mode) {
                RepeatMode.OFF -> Player.REPEAT_MODE_OFF
                RepeatMode.ONE -> Player.REPEAT_MODE_ONE
                RepeatMode.ALL -> Player.REPEAT_MODE_ALL
            }
    }

    override suspend fun setShuffleEnabled(enabled: Boolean): PlayerResult<Unit> = withController {
        it.shuffleModeEnabled = enabled
    }

    override suspend fun setPlaybackSpeed(speed: Float): PlayerResult<Unit> {
        if (speed !in MIN_PLAYBACK_SPEED..MAX_PLAYBACK_SPEED) return invalidQueue()
        return withController { it.setPlaybackSpeed(speed) }
    }

    override suspend fun setSleepTimer(durationMs: Long?): PlayerResult<Unit> {
        if (durationMs != null && durationMs !in 1L..MAX_SLEEP_TIMER_DURATION_MS) {
            return PlayerResult.Failure(
                PlayerFailure.Unexpected("Sleep timer duration is out of range"),
            )
        }
        val args =
            Bundle().apply {
                putLong(PlaybackSessionCommands.ARG_SLEEP_TIMER_DURATION_MS, durationMs ?: 0L)
            }
        return withController { controller ->
            if (!controller.isSessionCommandAvailable(PlaybackSessionCommands.SET_SLEEP_TIMER)) {
                return@withController PlayerResult.Failure(
                    PlayerFailure.Unexpected("Sleep timer command is unavailable"),
                )
            }
            val result =
                controller
                    .sendCustomCommand(
                        PlaybackSessionCommands.SET_SLEEP_TIMER,
                        args,
                    ).awaitFuture()
            if (result.resultCode != SessionResult.RESULT_SUCCESS) {
                return@withController PlayerResult.Failure(
                    PlayerFailure.Unexpected("Sleep timer command failed: ${result.resultCode}"),
                )
            }
            updateSleepTimerDeadline(
                PlaybackSessionCommands.sleepTimerDeadline(result.extras),
            )
        }
    }

    private suspend fun withController(
        rebuildQueueAfterCommand: Boolean = false,
        positionDiscontinuity: () -> Boolean = { false },
        command: suspend (MediaController) -> Any?,
    ): PlayerResult<Unit> = withContext(Dispatchers.Main.immediate) {
        val controller =
            connection.current() ?: run {
                when (connect()) {
                    is PlayerResult.Success -> connection.current()
                    is PlayerResult.Failure -> null
                }
            } ?: return@withContext PlayerResult.Failure(PlayerFailure.ConnectionUnavailable)
        try {
            val result = command(controller)
            updateState(
                player = controller,
                rebuildQueue = rebuildQueueAfterCommand,
                positionDiscontinuity =
                    shouldMarkPositionDiscontinuity(
                        commandSucceeded = result !is PlayerResult.Failure,
                        didChangePosition = positionDiscontinuity(),
                    ),
            )
            if (result is PlayerResult.Failure) result else PlayerResult.Success(Unit)
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: Exception) {
            PlayerResult.Failure(PlayerFailure.Unexpected(null))
        }
    }

    private fun attach(controller: MediaController) {
        if (attachedController === controller) return
        detachController()
        attachedController = controller
        controller.addListener(listener)
        updateState(controller, rebuildQueue = true)
        refreshSleepTimerState(controller)
    }

    private fun onControllerDisconnected(controller: MediaController) {
        scope.launch {
            if (!detachController(controller)) return@launch
            mutableState.value = disconnectedPlayerState()
        }
    }

    private fun detachController(expectedController: MediaController? = attachedController): Boolean {
        val controller = attachedController
        if (controller == null || controller !== expectedController) return false
        stopPositionUpdates()
        clearSleepTimerTracking()
        queueMappingJob?.cancel()
        queueMappingJob = null
        queueMappingGeneration++
        controller.removeListener(listener)
        attachedController = null
        return true
    }

    private fun updateProgress(player: Player) {
        val previous = mutableState.value
        val positionMs = player.currentPosition.coerceAtLeast(0)
        val bufferedPositionMs = player.bufferedPosition.coerceAtLeast(0)
        val durationMs = player.duration.takeUnless { it == C.TIME_UNSET }?.coerceAtLeast(0) ?: 0
        mutableState.value =
            playerStateWithProgressSample(
                previous = previous,
                positionMs = positionMs,
                bufferedPositionMs = bufferedPositionMs,
                durationMs = durationMs,
                sampledAtElapsedRealtimeMs = SystemClock.elapsedRealtime(),
            )
    }

    private fun updateState(
        player: Player,
        rebuildQueue: Boolean,
        positionDiscontinuity: Boolean = false,
    ) {
        if (rebuildQueue) scheduleQueueMapping(player)
        val previous = mutableState.value
        val currentQueueItemId = player.currentMediaItem?.mediaId
        val isPlaying = player.isPlaying
        val sampledAtElapsedRealtimeMs = SystemClock.elapsedRealtime()
        mutableState.value =
            PlayerState(
                connectionState = PlayerConnectionState.CONNECTED,
                playbackState =
                when (player.playbackState) {
                    Player.STATE_BUFFERING -> PlaybackState.BUFFERING
                    Player.STATE_READY -> PlaybackState.READY
                    Player.STATE_ENDED -> PlaybackState.ENDED
                    else -> PlaybackState.IDLE
                },
                queue = previous.queue,
                currentQueueItemId = currentQueueItemId,
                isPlaying = isPlaying,
                positionMs = player.currentPosition.coerceAtLeast(0),
                positionAnchorElapsedRealtimeMs = sampledAtElapsedRealtimeMs,
                positionDiscontinuitySequence =
                nextPositionDiscontinuitySequence(
                    previous = previous,
                    currentQueueItemId = currentQueueItemId,
                    explicitDiscontinuity = positionDiscontinuity,
                ),
                bufferedPositionMs = player.bufferedPosition.coerceAtLeast(0),
                durationMs = player.duration.takeUnless { it == C.TIME_UNSET }?.coerceAtLeast(0) ?: 0,
                repeatMode =
                when (player.repeatMode) {
                    Player.REPEAT_MODE_ONE -> RepeatMode.ONE
                    Player.REPEAT_MODE_ALL -> RepeatMode.ALL
                    else -> RepeatMode.OFF
                },
                shuffleEnabled = player.shuffleModeEnabled,
                playbackSpeed = player.playbackParameters.speed,
                sleepTimerRemainingMs = previous.sleepTimerRemainingMs,
                failure = player.playerError?.let { PlayerFailure.Unexpected(it.errorCodeName) },
            )
        syncPositionUpdates(player)
    }

    private fun refreshSleepTimerState(controller: MediaController) {
        val generation = sleepTimerSyncGeneration
        scope.launch {
            if (!controller.isSessionCommandAvailable(PlaybackSessionCommands.GET_SLEEP_TIMER)) {
                clearSleepTimerTrackingIfAttached(controller)
                return@launch
            }
            try {
                val result =
                    controller
                        .sendCustomCommand(
                            PlaybackSessionCommands.GET_SLEEP_TIMER,
                            Bundle.EMPTY,
                        ).awaitFuture()
                applySleepTimerState(controller, generation, result)
            } catch (failure: CancellationException) {
                throw failure
            } catch (_: Exception) {
                clearSleepTimerTrackingIfCurrent(controller, generation)
            }
        }
    }

    private fun applySleepTimerState(controller: MediaController, generation: Long, result: SessionResult) {
        if (!isCurrentSleepTimerSync(controller, generation)) return
        if (result.resultCode == SessionResult.RESULT_SUCCESS) {
            updateSleepTimerDeadline(
                PlaybackSessionCommands.sleepTimerDeadline(result.extras),
            )
        } else {
            clearSleepTimerTracking()
        }
    }

    private fun clearSleepTimerTrackingIfAttached(controller: MediaController) {
        if (controller === attachedController) clearSleepTimerTracking()
    }

    private fun clearSleepTimerTrackingIfCurrent(controller: MediaController, generation: Long) {
        if (isCurrentSleepTimerSync(controller, generation)) clearSleepTimerTracking()
    }

    private fun isCurrentSleepTimerSync(controller: MediaController, generation: Long): Boolean =
        controller === attachedController && generation == sleepTimerSyncGeneration

    private fun updateSleepTimerDeadline(deadlineElapsedRealtimeMs: Long?) {
        if (sleepTimerDeadlineElapsedRealtimeMs == deadlineElapsedRealtimeMs) {
            publishSleepTimerRemaining()
            return
        }
        sleepTimerSyncGeneration++
        sleepTimerUpdateJob?.cancel()
        sleepTimerUpdateJob = null
        sleepTimerDeadlineElapsedRealtimeMs = deadlineElapsedRealtimeMs
        publishSleepTimerRemaining()
        val expectedDeadline = sleepTimerDeadlineElapsedRealtimeMs ?: return
        sleepTimerUpdateJob =
            scope.launch {
                while (isActive && sleepTimerDeadlineElapsedRealtimeMs == expectedDeadline) {
                    val remainingMs = publishSleepTimerRemaining()
                    if (remainingMs == null) {
                        sleepTimerSyncGeneration++
                        sleepTimerDeadlineElapsedRealtimeMs = null
                        break
                    }
                    delay(minOf(remainingMs, SLEEP_TIMER_UPDATE_INTERVAL_MS))
                }
            }
    }

    private fun publishSleepTimerRemaining(): Long? {
        val remainingMs =
            remainingSleepTimerMs(
                sleepTimerDeadlineElapsedRealtimeMs,
                SystemClock.elapsedRealtime(),
            )
        val previous = mutableState.value
        if (previous.sleepTimerRemainingMs != remainingMs) {
            mutableState.value = previous.copy(sleepTimerRemainingMs = remainingMs)
        }
        return remainingMs
    }

    private fun clearSleepTimerTracking() {
        sleepTimerSyncGeneration++
        sleepTimerUpdateJob?.cancel()
        sleepTimerUpdateJob = null
        sleepTimerDeadlineElapsedRealtimeMs = null
        val previous = mutableState.value
        if (previous.sleepTimerRemainingMs != null) {
            mutableState.value = previous.copy(sleepTimerRemainingMs = null)
        }
    }

    private fun scheduleQueueMapping(player: Player) {
        val mediaItems = List(player.mediaItemCount, player::getMediaItemAt)
        val generation = ++queueMappingGeneration
        queueMappingJob?.cancel()
        queueMappingJob =
            scope.launch(defaultDispatcher) {
                val queue = mediaItems.map(::fromMediaItem)
                withContext(Dispatchers.Main.immediate) {
                    if (generation == queueMappingGeneration && player === attachedController) {
                        mutableState.value = mutableState.value.copy(queue = queue)
                    }
                }
            }
    }

    private fun syncPositionUpdates(player: Player) {
        if (
            player !== attachedController ||
            !shouldSamplePlaybackPosition(
                isPlaying = player.isPlaying,
                hasCurrentMediaItem = player.currentMediaItem != null,
            )
        ) {
            stopPositionUpdates()
            return
        }
        if (positionUpdateJob?.isActive == true) return
        positionUpdateJob =
            scope.launch {
                while (
                    isActive &&
                    player === attachedController &&
                    shouldSamplePlaybackPosition(
                        isPlaying = player.isPlaying,
                        hasCurrentMediaItem = player.currentMediaItem != null,
                    )
                ) {
                    delay(POSITION_UPDATE_INTERVAL_MS)
                    if (isActive && player === attachedController && player.isPlaying) {
                        updateProgress(player)
                    }
                }
            }
    }

    private fun stopPositionUpdates() {
        positionUpdateJob?.cancel()
        positionUpdateJob = null
    }

    private fun toMediaItem(item: PlayerQueueItem): MediaItem {
        val extras =
            Bundle().apply {
                putString(PlaybackMediaMetadata.EXTRA_TRACK_ID, item.trackId)
                putStringArrayList(PlaybackMediaMetadata.EXTRA_ARTISTS, ArrayList(item.artistNames))
                putString(PlaybackMediaMetadata.EXTRA_ARTWORK_CACHE_KEY, item.artworkCacheKey)
                putLong(PlaybackMediaMetadata.EXTRA_DURATION_MS, item.durationMs)
            }
        val metadata =
            MediaMetadata
                .Builder()
                .setTitle(item.title)
                .setArtist(item.artistNames.joinToString(" / "))
                .setAlbumTitle(item.albumTitle)
                .setArtworkUri(item.artworkUrl?.let(Uri::parse))
                .setExtras(extras)
                .build()
        return MediaItem
            .Builder()
            .setMediaId(item.queueItemId)
            .setUri(PlaybackMediaUri.forTrack(item.trackId))
            .setMediaMetadata(metadata)
            .build()
    }

    private fun fromMediaItem(item: MediaItem): PlayerQueueItem {
        val extras = item.mediaMetadata.extras
        val trackId =
            extras?.getString(PlaybackMediaMetadata.EXTRA_TRACK_ID) ?: runCatching {
                PlaybackMediaUri.trackId(requireNotNull(item.localConfiguration).uri)
            }.getOrDefault("")
        return PlayerQueueItem(
            queueItemId = item.mediaId,
            trackId = trackId,
            title =
            item.mediaMetadata.title
                ?.toString()
                .orEmpty(),
            artistNames = extras?.getStringArrayList(PlaybackMediaMetadata.EXTRA_ARTISTS).orEmpty(),
            albumTitle = item.mediaMetadata.albumTitle?.toString(),
            artworkUrl = item.mediaMetadata.artworkUri?.toString(),
            artworkCacheKey = extras?.getString(PlaybackMediaMetadata.EXTRA_ARTWORK_CACHE_KEY),
            durationMs = extras?.getLong(PlaybackMediaMetadata.EXTRA_DURATION_MS) ?: 0,
        )
    }

    private fun indexOf(player: Player, queueItemId: String): Int = (0 until player.mediaItemCount).firstOrNull {
        player.getMediaItemAt(it).mediaId == queueItemId
    } ?: -1

    private fun invalidQueue(): PlayerResult.Failure = PlayerResult.Failure(PlayerFailure.InvalidQueue)

    private companion object {
        const val POSITION_UPDATE_INTERVAL_MS = 1_000L
        const val SLEEP_TIMER_UPDATE_INTERVAL_MS = 1_000L
        const val MIN_PLAYBACK_SPEED = 0.5f
        const val MAX_PLAYBACK_SPEED = 2f
    }
}

internal fun playerEventForCustomAction(customAction: String): PlayerEvent? = when (customAction) {
    PlaybackSessionCommands.ACTION_CODEC_FALLBACK_APPLIED ->
        PlayerEvent.CompatibleCodecFallbackApplied
    else -> null
}

internal fun shouldSamplePlaybackPosition(isPlaying: Boolean, hasCurrentMediaItem: Boolean): Boolean =
    isPlaying && hasCurrentMediaItem

internal fun remainingSleepTimerMs(deadlineElapsedRealtimeMs: Long?, nowElapsedRealtimeMs: Long): Long? =
    deadlineElapsedRealtimeMs
        ?.takeIf { it > nowElapsedRealtimeMs }
        ?.minus(nowElapsedRealtimeMs)

internal fun playerStateWithProgressSample(
    previous: PlayerState,
    positionMs: Long,
    bufferedPositionMs: Long,
    durationMs: Long,
    sampledAtElapsedRealtimeMs: Long,
): PlayerState =
    previous.copy(
        positionMs = positionMs.coerceAtLeast(0),
        positionAnchorElapsedRealtimeMs = sampledAtElapsedRealtimeMs.coerceAtLeast(0),
        bufferedPositionMs = bufferedPositionMs.coerceAtLeast(0),
        durationMs = durationMs.coerceAtLeast(0),
    )

internal fun nextPositionDiscontinuitySequence(
    previous: PlayerState,
    currentQueueItemId: String?,
    explicitDiscontinuity: Boolean,
): Long =
    previous.positionDiscontinuitySequence +
        if (
            explicitDiscontinuity ||
                previous.currentQueueItemId != currentQueueItemId
        ) {
            1L
        } else {
            0L
        }

internal fun shouldMarkPositionDiscontinuity(
    commandSucceeded: Boolean,
    didChangePosition: Boolean,
): Boolean = commandSucceeded && didChangePosition

internal fun disconnectedPlayerState(): PlayerState = PlayerState(
    failure = PlayerFailure.ConnectionUnavailable,
)

internal fun isValidPlayerQueue(
    items: List<PlayerQueueItem>,
    startQueueItemId: String?,
    startPositionMs: Long,
): Boolean = runCatching {
    require(startPositionMs >= 0)
    require(items.map { it.queueItemId }.all(String::isNotBlank))
    require(items.map { it.queueItemId }.distinct().size == items.size)
    items.forEach {
        UUID.fromString(it.trackId)
        require(it.durationMs > 0)
    }
    require(startQueueItemId == null || items.any { it.queueItemId == startQueueItemId })
}.isSuccess
