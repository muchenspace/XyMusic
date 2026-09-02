package com.xymusic.app.feature.player.service

import android.net.Uri
import android.os.Bundle
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.Player
import com.xymusic.app.feature.player.adapter.media3.PlaybackMediaMetadata
import com.xymusic.app.feature.player.adapter.media3.PlaybackMediaUri
import com.xymusic.app.feature.player.domain.StoredPlaybackQueueItem
import java.util.UUID

internal class PlaybackQueueRestorer(private val player: Player) {
    fun restore(storedItems: List<StoredPlaybackQueueItem>, onItemRestored: (StoredPlaybackQueueItem) -> Unit) {
        val restored = selectRestorablePlaybackQueue(storedItems) ?: return
        val mediaItems =
            restored.items.map { stored ->
                onItemRestored(stored)
                stored.toPlaybackMediaItem()
            }
        player.setMediaItems(
            mediaItems,
            restored.currentIndex,
            restored.startPositionMs,
        )
        player.playWhenReady = false
    }
}

internal fun StoredPlaybackQueueItem.toPlaybackMediaItem(): MediaItem = MediaItem
    .Builder()
    .setMediaId(queueItemId)
    .setUri(PlaybackMediaUri.forTrack(trackId))
    .setMediaMetadata(toMediaMetadata(requestedStartPositionMs = resumePositionMs.coerceAtLeast(0)))
    .build()

private fun StoredPlaybackQueueItem.toMediaMetadata(requestedStartPositionMs: Long): MediaMetadata {
    val extras =
        Bundle().apply {
            putString(PlaybackMediaMetadata.EXTRA_TRACK_ID, trackId)
            putStringArrayList(PlaybackMediaMetadata.EXTRA_ARTISTS, ArrayList(artistNames))
            putString(PlaybackMediaMetadata.EXTRA_ARTWORK_CACHE_KEY, artworkCacheKey)
            putLong(PlaybackMediaMetadata.EXTRA_DURATION_MS, durationMs)
            if (isCurrent && requestedStartPositionMs > 0) {
                putLong(PlaybackMediaMetadata.EXTRA_REQUESTED_START_POSITION_MS, requestedStartPositionMs)
                // The source offset is declared with the request, not with the
                // resolution result. The served HLS transcode begins at the
                // resume position, so the local timeline starts at zero while
                // the global position is the resume position from the first
                // sample. Readers (lyrics clock, persistence, progress) never
                // observe a transient 0 while the media source resolves.
                putLong(PlaybackMediaMetadata.EXTRA_SOURCE_OFFSET_MS, requestedStartPositionMs)
            }
        }
    return MediaMetadata
        .Builder()
        .setTitle(title.ifBlank { trackId })
        .setArtist(artistNames.joinToString(" / "))
        .setAlbumTitle(albumTitle)
        .setArtworkUri(artworkUrl?.let(Uri::parse))
        .setExtras(extras)
        .build()
}

internal data class RestorablePlaybackQueue(
    val items: List<StoredPlaybackQueueItem>,
    val currentIndex: Int,
    val startPositionMs: Long,
)

internal fun selectRestorablePlaybackQueue(storedItems: List<StoredPlaybackQueueItem>): RestorablePlaybackQueue? {
    val orderedItems = storedItems.sortedBy(StoredPlaybackQueueItem::position)
    val preferredCurrentId =
        orderedItems
            .firstOrNull(StoredPlaybackQueueItem::isCurrent)
            ?.queueItemId
    val restorableItems =
        orderedItems.filter { item ->
            item.queueItemId.isNotBlank() && runCatching { UUID.fromString(item.trackId) }.isSuccess
        }
    if (restorableItems.isEmpty()) return null
    val currentIndex =
        restorableItems
            .indexOfFirst { it.queueItemId == preferredCurrentId }
            .takeIf { it >= 0 }
            ?: 0
    return RestorablePlaybackQueue(
        items = restorableItems,
        currentIndex = currentIndex,
        startPositionMs = restorableItems[currentIndex].restorablePositionMs(),
    )
}

private fun StoredPlaybackQueueItem.restorablePositionMs(): Long {
    val nonNegativePosition = resumePositionMs.coerceAtLeast(0)
    val completedBoundaryMs = (durationMs - RESTORE_COMPLETION_TOLERANCE_MS).coerceAtLeast(0)
    return if (durationMs > 0 && nonNegativePosition >= completedBoundaryMs) 0 else nonNegativePosition
}

private const val RESTORE_COMPLETION_TOLERANCE_MS = 1_000L
