package com.xymusic.app.feature.player.adapter.media3

import androidx.media3.common.MediaItem
import java.util.UUID

internal fun MediaItem.globalPlaybackPositionMs(localPositionMs: Long): Long =
    localPositionMs.coerceAtLeast(0)

internal fun MediaItem.playbackMetadataDurationMs(): Long {
    val standardDurationMs = mediaMetadata.durationMs?.takeIf { it > 0 } ?: 0
    val legacyDurationMs =
        mediaMetadata.extras
            ?.getLong(PlaybackMediaMetadata.EXTRA_DURATION_MS)
            ?.takeIf { it > 0 }
            ?: 0
    return maxOf(standardDurationMs, legacyDurationMs)
}

internal fun MediaItem.globalPlaybackDurationMs(localDurationMs: Long): Long {
    val resolvedLocalDurationMs = localDurationMs.coerceAtLeast(0)
    return maxOf(
        resolvedLocalDurationMs,
        playbackMetadataDurationMs(),
    )
}

internal fun MediaItem.playbackTrackId(): String? {
    val metadataTrackId =
        mediaMetadata.extras
            ?.getString(PlaybackMediaMetadata.EXTRA_TRACK_ID)
            ?.validTrackId()
    if (metadataTrackId != null) return metadataTrackId
    return runCatching {
        PlaybackMediaUri.trackId(requireNotNull(localConfiguration).uri)
    }.getOrNull()
}

private fun String.validTrackId(): String? = runCatching {
    UUID.fromString(this)
    this
}.getOrNull()
