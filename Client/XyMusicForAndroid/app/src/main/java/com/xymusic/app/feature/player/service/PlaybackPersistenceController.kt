package com.xymusic.app.feature.player.service

import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.common.Timeline
import com.xymusic.app.feature.player.data.media.PlaybackMediaMetadata
import com.xymusic.app.feature.player.data.media.PlaybackMediaUri
import com.xymusic.app.feature.player.domain.PlaybackCheckpoint
import com.xymusic.app.feature.player.domain.PlaybackEventSink
import com.xymusic.app.feature.player.domain.PlaybackEventType
import com.xymusic.app.feature.player.domain.PlaybackQueueStore
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.StoredPlaybackQueueItem
import java.time.Clock
import java.util.Collections
import java.util.UUID
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

internal class PlaybackPersistenceController(
    private val player: Player,
    private val serviceScope: CoroutineScope,
    private val queueStore: PlaybackQueueStore,
    private val eventSink: PlaybackEventSink,
    private val clock: Clock,
    private val cancelSleepTimer: () -> Unit,
    private val clearPlaybackGrants: () -> Unit,
) {
    private val persistenceMutex = Mutex()
    private val persistenceJobs = Collections.synchronizedSet(mutableSetOf<Job>())
    private val enqueuedAtByQueueItemId = mutableMapOf<String, Long>()
    private val queueRestorer = PlaybackQueueRestorer(player)

    private var checkpointJob: Job? = null
    private var structuralPersistenceJob: Job? = null
    private var finalFlushJob: Job? = null
    private var activeUserId: String? = null
    private var suppressPersistence = true
    private var activePlaybackSessionId: String? = null
    private var activePlaybackQueueItemId: String? = null
    private var lastCheckpoint: PlaybackCheckpoint? = null
    private var persistenceGeneration = 0L

    fun isActiveUser(userId: String): Boolean = activeUserId == userId

    fun clearForAccountChange(nextUserId: String?) {
        suppressPersistence = true
        persistenceGeneration += 1
        checkpointJob?.cancel()
        cancelSleepTimer()
        structuralPersistenceJob?.cancel()
        finalFlushJob?.cancel()
        val jobsToCancel =
            synchronized(persistenceJobs) {
                persistenceJobs.toList().also { persistenceJobs.clear() }
            }
        jobsToCancel.forEach(Job::cancel)
        player.stop()
        player.clearMediaItems()
        clearPlaybackGrants()
        enqueuedAtByQueueItemId.clear()
        resetPlaybackSession()
        activeUserId = nextUserId
    }

    suspend fun restoreQueue() {
        val storedItems = queueStore.observe().first()
        queueRestorer.restore(storedItems) { stored ->
            enqueuedAtByQueueItemId[stored.queueItemId] = stored.enqueuedAtEpochMillis
        }
        suppressPersistence = false
    }

    fun flushForTaskRemoval(stopAfterFlush: Boolean, onFlushed: () -> Unit) {
        structuralPersistenceJob?.cancel()
        finalFlushJob?.cancel()
        val checkpoint = currentCheckpoint(PlaybackEventType.PROGRESS)
        val target = currentPersistenceTarget()
        finalFlushJob =
            launchPersistence {
                if (target != null) persistSafely(target, checkpoint)
                if (stopAfterFlush) onFlushed()
            }
    }

    fun cancelForDestroy(cancelSleepTimerJob: () -> Unit) {
        checkpointJob?.cancel()
        cancelSleepTimerJob()
        structuralPersistenceJob?.cancel()
        finalFlushJob?.cancel()
    }

    private fun scheduleCheckpoint(checkpoint: PlaybackCheckpoint, markCurrent: Boolean) {
        val target = currentPersistenceTarget() ?: return
        launchPersistence { persistCheckpointSafely(target, checkpoint, markCurrent) }
    }

    private fun scheduleStructuralPersistence() {
        val target = currentPersistenceTarget() ?: return
        structuralPersistenceJob?.cancel()
        structuralPersistenceJob =
            launchPersistence {
                delay(STRUCTURAL_PERSISTENCE_DEBOUNCE_MS)
                persistSafely(target, checkpoint = null)
            }
    }

    private fun scheduleCurrentCheckpoint(event: PlaybackEventType) {
        val checkpoint = currentCheckpoint(event) ?: return
        scheduleCheckpoint(checkpoint, markCurrent = true)
    }

    private fun scheduleCurrentState() {
        val target = currentPersistenceTarget() ?: return
        val queueItemId = player.currentMediaItem?.mediaId ?: return
        val positionMs = player.currentPosition.coerceAtLeast(0)
        launchPersistence {
            persistCurrentStateSafely(target, queueItemId, positionMs)
        }
    }

    private suspend fun persistProgressSafely(event: PlaybackEventType) {
        val target = currentPersistenceTarget() ?: return
        val checkpoint = currentCheckpoint(event) ?: return
        persistCheckpointSafely(target, checkpoint, markCurrent = false)
    }

    private suspend fun persistCheckpointSafely(
        target: PersistenceTarget,
        checkpoint: PlaybackCheckpoint,
        markCurrent: Boolean,
    ) {
        try {
            persistenceMutex.withLock {
                if (!isCurrentPersistenceTarget(target)) return@withLock
                val updateResult =
                    if (markCurrent) {
                        queueStore.setCurrent(
                            ownerUserId = target.ownerUserId,
                            queueItemId = checkpoint.queueItemId,
                            positionMs = checkpoint.positionMs,
                        )
                    } else {
                        queueStore.updatePosition(
                            ownerUserId = target.ownerUserId,
                            queueItemId = checkpoint.queueItemId,
                            positionMs = checkpoint.positionMs,
                        )
                    }
                if (updateResult is PlayerResult.Success) {
                    eventSink.record(target.ownerUserId, checkpoint)
                } else {
                    persistQueueLocked(target, checkpoint)
                }
            }
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: Exception) {
            // A later checkpoint or structural queue persistence will retry.
        }
    }

    private suspend fun persistCurrentStateSafely(target: PersistenceTarget, queueItemId: String, positionMs: Long) {
        try {
            persistenceMutex.withLock {
                if (!isCurrentPersistenceTarget(target)) return@withLock
                if (
                    queueStore.setCurrent(
                        ownerUserId = target.ownerUserId,
                        queueItemId = queueItemId,
                        positionMs = positionMs,
                    ) !is PlayerResult.Success
                ) {
                    persistQueueLocked(target, checkpoint = null)
                }
            }
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: Exception) {
            // A later state transition or structural queue persistence will retry.
        }
    }

    private suspend fun persistSafely(target: PersistenceTarget, checkpoint: PlaybackCheckpoint?) {
        try {
            persistQueue(target, checkpoint)
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: Exception) {
            // The next state transition or periodic checkpoint will retry local persistence.
        }
    }

    private suspend fun persistQueue(target: PersistenceTarget, checkpoint: PlaybackCheckpoint?) =
        persistenceMutex.withLock {
            persistQueueLocked(target, checkpoint)
        }

    private suspend fun persistQueueLocked(target: PersistenceTarget, checkpoint: PlaybackCheckpoint?) {
        if (!isCurrentPersistenceTarget(target)) return
        val currentQueueItemId = player.currentMediaItem?.mediaId
        val currentPosition = player.currentPosition.coerceAtLeast(0)
        val now = clock.millis()
        var items =
            buildList {
                repeat(player.mediaItemCount) { index ->
                    val mediaItem = player.getMediaItemAt(index)
                    if (mediaItem.mediaId.isBlank()) return@repeat
                    val trackId =
                        runCatching {
                            PlaybackMediaUri.trackId(requireNotNull(mediaItem.localConfiguration).uri)
                        }.getOrNull() ?: return@repeat
                    add(
                        StoredPlaybackQueueItem(
                            queueItemId = mediaItem.mediaId,
                            position = size,
                            trackId = trackId,
                            variantId = null,
                            stableCacheKey = null,
                            resumePositionMs =
                            if (mediaItem.mediaId == currentQueueItemId) {
                                currentPosition
                            } else {
                                0
                            },
                            isCurrent = mediaItem.mediaId == currentQueueItemId,
                            enqueuedAtEpochMillis =
                            enqueuedAtByQueueItemId.getOrPut(mediaItem.mediaId) {
                                now
                            },
                            title =
                            mediaItem.mediaMetadata.title
                                ?.toString()
                                .orEmpty()
                                .ifBlank { trackId },
                            artistNames =
                            mediaItem.mediaMetadata.extras
                                ?.getStringArrayList(PlaybackMediaMetadata.EXTRA_ARTISTS)
                                .orEmpty(),
                            albumTitle = mediaItem.mediaMetadata.albumTitle?.toString(),
                            artworkUrl = mediaItem.mediaMetadata.artworkUri?.toString(),
                            artworkCacheKey =
                            mediaItem.mediaMetadata.extras
                                ?.getString(PlaybackMediaMetadata.EXTRA_ARTWORK_CACHE_KEY),
                            durationMs =
                            mediaItem.mediaMetadata.extras
                                ?.getLong(PlaybackMediaMetadata.EXTRA_DURATION_MS)
                                ?.coerceAtLeast(0)
                                ?: 0,
                        ),
                    )
                }
            }
        if (items.isNotEmpty() && items.none(StoredPlaybackQueueItem::isCurrent)) {
            items =
                items.mapIndexed { index, item ->
                    if (index == 0) item.copy(isCurrent = true, resumePositionMs = 0) else item
                }
        }
        enqueuedAtByQueueItemId.keys.retainAll(items.mapTo(mutableSetOf()) { it.queueItemId })
        if (!isCurrentPersistenceTarget(target)) return
        if (queueStore.replace(target.ownerUserId, items) is PlayerResult.Success) {
            checkpoint?.let { eventSink.record(target.ownerUserId, it) }
        }
    }

    private fun currentPersistenceTarget(): PersistenceTarget? = activeUserId
        ?.takeUnless { suppressPersistence }
        ?.let { ownerUserId -> PersistenceTarget(ownerUserId, persistenceGeneration) }

    private fun isCurrentPersistenceTarget(target: PersistenceTarget): Boolean = !suppressPersistence &&
        activeUserId == target.ownerUserId &&
        persistenceGeneration == target.generation

    private fun launchPersistence(block: suspend () -> Unit): Job {
        val job = serviceScope.launch { block() }
        persistenceJobs += job
        job.invokeOnCompletion { persistenceJobs -= job }
        return job
    }

    private fun currentCheckpoint(event: PlaybackEventType): PlaybackCheckpoint? {
        val mediaItem = player.currentMediaItem ?: return null
        val sessionId = activePlaybackSessionId ?: return null
        if (activePlaybackQueueItemId != mediaItem.mediaId) return null
        val trackId =
            runCatching {
                PlaybackMediaUri.trackId(requireNotNull(mediaItem.localConfiguration).uri)
            }.getOrNull() ?: return null
        return PlaybackCheckpoint(
            playbackSessionId = sessionId,
            queueItemId = mediaItem.mediaId,
            trackId = trackId,
            positionMs = player.currentPosition.coerceAtLeast(0),
            durationMs = player.duration.takeUnless { it == C.TIME_UNSET }?.coerceAtLeast(0) ?: 0,
            occurredAtEpochMillis = clock.millis(),
            event = event,
        ).also { lastCheckpoint = it }
    }

    private fun startPlaybackSessionIfNeeded(): Boolean {
        val queueItemId = player.currentMediaItem?.mediaId ?: return false
        if (activePlaybackSessionId != null && activePlaybackQueueItemId == queueItemId) return false
        activePlaybackSessionId = UUID.randomUUID().toString()
        activePlaybackQueueItemId = queueItemId
        lastCheckpoint = null
        return true
    }

    private fun resetPlaybackSession() {
        activePlaybackSessionId = null
        activePlaybackQueueItemId = null
        lastCheckpoint = null
    }

    private fun startPeriodicCheckpoints() {
        checkpointJob?.cancel()
        checkpointJob =
            serviceScope.launch {
                while (isActive) {
                    delay(CHECKPOINT_INTERVAL_MS)
                    persistProgressSafely(PlaybackEventType.PROGRESS)
                }
            }
    }

    private val playerListener =
        object : Player.Listener {
            override fun onTimelineChanged(timeline: Timeline, reason: Int) {
                scheduleStructuralPersistence()
            }

            override fun onPositionDiscontinuity(
                oldPosition: Player.PositionInfo,
                newPosition: Player.PositionInfo,
                reason: Int,
            ) {
                if (oldPosition.mediaItemIndex == newPosition.mediaItemIndex) return
                val transitionEvent =
                    if (reason == Player.DISCONTINUITY_REASON_AUTO_TRANSITION) {
                        PlaybackEventType.COMPLETED
                    } else {
                        PlaybackEventType.PROGRESS
                    }
                lastCheckpoint
                    ?.copy(
                        positionMs = oldPosition.positionMs.coerceAtLeast(0),
                        occurredAtEpochMillis = clock.millis(),
                        event = transitionEvent,
                    )?.let { checkpoint ->
                        scheduleCheckpoint(checkpoint, markCurrent = false)
                    }
                resetPlaybackSession()
            }

            override fun onIsPlayingChanged(isPlaying: Boolean) {
                if (isPlaying) {
                    if (startPlaybackSessionIfNeeded()) {
                        scheduleCurrentCheckpoint(PlaybackEventType.STARTED)
                    } else {
                        scheduleCurrentState()
                    }
                    startPeriodicCheckpoints()
                } else {
                    checkpointJob?.cancel()
                    if (!player.playWhenReady && activePlaybackSessionId != null) {
                        scheduleCurrentCheckpoint(PlaybackEventType.PAUSED)
                    }
                }
            }

            override fun onMediaItemTransition(mediaItem: MediaItem?, reason: Int) {
                val queueItemChanged =
                    activePlaybackQueueItemId != null &&
                        activePlaybackQueueItemId != mediaItem?.mediaId
                val repeated = reason == Player.MEDIA_ITEM_TRANSITION_REASON_REPEAT
                if (queueItemChanged || repeated) {
                    lastCheckpoint
                        ?.copy(
                            occurredAtEpochMillis = clock.millis(),
                            event =
                            if (reason == Player.MEDIA_ITEM_TRANSITION_REASON_AUTO || repeated) {
                                PlaybackEventType.COMPLETED
                            } else {
                                PlaybackEventType.PROGRESS
                            },
                        )?.let { checkpoint ->
                            scheduleCheckpoint(checkpoint, markCurrent = false)
                        }
                    resetPlaybackSession()
                }
                if (player.isPlaying && startPlaybackSessionIfNeeded()) {
                    scheduleCurrentCheckpoint(PlaybackEventType.STARTED)
                } else {
                    scheduleCurrentState()
                }
            }

            override fun onPlaybackStateChanged(playbackState: Int) {
                if (playbackState == Player.STATE_ENDED) {
                    checkpointJob?.cancel()
                    val completed = currentCheckpoint(PlaybackEventType.COMPLETED)
                    completed?.let { checkpoint ->
                        scheduleCheckpoint(checkpoint, markCurrent = true)
                    }
                    resetPlaybackSession()
                }
            }
        }

    init {
        player.addListener(playerListener)
    }

    private companion object {
        const val CHECKPOINT_INTERVAL_MS = 15_000L
        const val STRUCTURAL_PERSISTENCE_DEBOUNCE_MS = 250L
    }

    private data class PersistenceTarget(val ownerUserId: String, val generation: Long)
}
