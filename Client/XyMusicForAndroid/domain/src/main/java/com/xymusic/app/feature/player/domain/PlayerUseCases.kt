package com.xymusic.app.feature.player.domain

import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
import com.xymusic.app.feature.player.domain.model.RepeatMode
import javax.inject.Inject

class PlayerUseCases
@Inject
constructor(private val repository: PlayerRepository) {
    val state = repository.state
    val events = repository.events

    suspend fun connect() = repository.connect()

    suspend fun disconnect() = repository.disconnect()

    suspend fun setQueue(
        items: List<PlayerQueueItem>,
        startQueueItemId: String?,
        startPositionMs: Long = 0,
        playWhenReady: Boolean = false,
    ) = repository.setQueue(items, startQueueItemId, startPositionMs, playWhenReady)

    suspend fun addToQueue(items: List<PlayerQueueItem>) = repository.addToQueue(items)

    suspend fun removeFromQueue(queueItemId: String) = repository.removeFromQueue(queueItemId)

    suspend fun moveQueueItem(queueItemId: String, newIndex: Int) = repository.moveQueueItem(queueItemId, newIndex)

    suspend fun clearQueue() = repository.clearQueue()

    suspend fun play() = repository.play()

    suspend fun pause() = repository.pause()

    suspend fun seekTo(positionMs: Long) = repository.seekTo(positionMs)

    suspend fun seekToQueueItem(queueItemId: String, positionMs: Long = 0) =
        repository.seekToQueueItem(queueItemId, positionMs)

    suspend fun skipToNext() = repository.skipToNext()

    suspend fun skipToPrevious() = repository.skipToPrevious()

    suspend fun setRepeatMode(mode: RepeatMode) = repository.setRepeatMode(mode)

    suspend fun setShuffleEnabled(enabled: Boolean) = repository.setShuffleEnabled(enabled)

    suspend fun setPlaybackSpeed(speed: Float) = repository.setPlaybackSpeed(speed)

    suspend fun setSleepTimer(durationMs: Long?) = repository.setSleepTimer(durationMs)
}
