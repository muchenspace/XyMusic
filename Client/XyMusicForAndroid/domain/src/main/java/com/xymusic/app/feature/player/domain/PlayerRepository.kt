package com.xymusic.app.feature.player.domain

import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
import com.xymusic.app.feature.player.domain.model.PlayerState
import com.xymusic.app.feature.player.domain.model.RepeatMode
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.emptyFlow

interface PlayerRepository {
    val state: StateFlow<PlayerState>
    val events: Flow<PlayerEvent>
        get() = emptyFlow()

    suspend fun connect(): PlayerResult<Unit>

    suspend fun disconnect()

    suspend fun setQueue(
        items: List<PlayerQueueItem>,
        startQueueItemId: String?,
        startPositionMs: Long = 0,
        playWhenReady: Boolean = false,
    ): PlayerResult<Unit>

    suspend fun addToQueue(items: List<PlayerQueueItem>): PlayerResult<Unit>

    suspend fun removeFromQueue(queueItemId: String): PlayerResult<Unit>

    suspend fun moveQueueItem(queueItemId: String, newIndex: Int): PlayerResult<Unit>

    suspend fun clearQueue(): PlayerResult<Unit>

    suspend fun play(): PlayerResult<Unit>

    suspend fun pause(): PlayerResult<Unit>

    suspend fun seekTo(positionMs: Long): PlayerResult<Unit>

    suspend fun seekToQueueItem(queueItemId: String, positionMs: Long = 0): PlayerResult<Unit>

    suspend fun skipToNext(): PlayerResult<Unit>

    suspend fun skipToPrevious(): PlayerResult<Unit>

    suspend fun setRepeatMode(mode: RepeatMode): PlayerResult<Unit>

    suspend fun setShuffleEnabled(enabled: Boolean): PlayerResult<Unit>

    suspend fun setPlaybackSpeed(speed: Float): PlayerResult<Unit>

    suspend fun setSleepTimer(durationMs: Long?): PlayerResult<Unit>
}

sealed interface PlayerEvent {
    data object CompatibleCodecFallbackApplied : PlayerEvent
}

sealed interface PlayerResult<out T> {
    data class Success<T>(val value: T) : PlayerResult<T>

    data class Failure(val failure: com.xymusic.app.feature.player.domain.model.PlayerFailure) : PlayerResult<Nothing>
}
