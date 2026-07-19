package com.xymusic.app.feature.player.domain

import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
import kotlinx.coroutines.flow.Flow

data class OfflineTrack(
    val trackId: String,
    val title: String,
    val artistNames: List<String>,
    val albumTitle: String?,
    val artworkUrl: String?,
    val artworkCacheKey: String?,
    val durationMs: Long,
    val downloadedAtEpochMillis: Long,
) {
    fun toQueueItem(queueItemId: String): PlayerQueueItem = PlayerQueueItem(
        queueItemId = queueItemId,
        trackId = trackId,
        title = title,
        artistNames = artistNames,
        albumTitle = albumTitle,
        artworkUrl = artworkUrl,
        artworkCacheKey = artworkCacheKey,
        durationMs = durationMs,
    )
}

interface OfflineTrackRepository {
    fun observeAll(): Flow<List<OfflineTrack>>

    fun observeDownloaded(trackId: String): Flow<Boolean>

    suspend fun download(trackId: String): OfflineTrackResult

    suspend fun remove(trackId: String): OfflineTrackResult
}

sealed interface OfflineTrackResult {
    data object Success : OfflineTrackResult

    data object Unavailable : OfflineTrackResult
}
