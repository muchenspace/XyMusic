package com.xymusic.app.feature.player.domain

import kotlinx.coroutines.flow.Flow

data class StoredPlaybackQueueItem(
    val queueItemId: String,
    val position: Int,
    val trackId: String,
    val variantId: String?,
    val stableCacheKey: String?,
    val resumePositionMs: Long,
    val isCurrent: Boolean,
    val enqueuedAtEpochMillis: Long,
    val title: String,
    val artistNames: List<String>,
    val albumTitle: String?,
    val artworkUrl: String?,
    val artworkCacheKey: String?,
    val durationMs: Long,
)

interface PlaybackQueueStore {
    fun observe(): Flow<List<StoredPlaybackQueueItem>>

    suspend fun replace(ownerUserId: String, items: List<StoredPlaybackQueueItem>): PlayerResult<Unit>

    suspend fun updatePosition(ownerUserId: String, queueItemId: String, positionMs: Long): PlayerResult<Unit>

    suspend fun setCurrent(ownerUserId: String, queueItemId: String, positionMs: Long): PlayerResult<Unit>

    suspend fun clear(ownerUserId: String): PlayerResult<Unit>
}
