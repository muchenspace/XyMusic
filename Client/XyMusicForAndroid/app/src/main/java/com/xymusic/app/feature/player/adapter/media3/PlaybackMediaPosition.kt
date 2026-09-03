package com.xymusic.app.feature.player.adapter.media3

import android.os.Bundle
import androidx.media3.common.MediaItem
import com.xymusic.app.feature.player.domain.PlaybackStreamProtocol

internal fun MediaItem.playbackStreamProtocol(): PlaybackStreamProtocol? =
    mediaMetadata.extras
        ?.getString(PlaybackMediaMetadata.EXTRA_STREAM_PROTOCOL)
        ?.trim()
        ?.uppercase()
        ?.let { value -> runCatching { PlaybackStreamProtocol.valueOf(value) }.getOrNull() }

internal fun MediaItem.playbackSourceOffsetMs(): Long =
    mediaMetadata.extras
        ?.getLong(PlaybackMediaMetadata.EXTRA_SOURCE_OFFSET_MS)
        ?.coerceAtLeast(0)
        ?: 0

internal fun MediaItem.globalPlaybackPositionMs(localPositionMs: Long): Long =
    localPositionMs.coerceAtLeast(0).saturatingAdd(playbackSourceOffsetMs())

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
    val resolvedLocalDurationMs =
        localDurationMs
            .takeIf { it > 0 }
            ?.saturatingAdd(playbackSourceOffsetMs())
            ?: 0
    return maxOf(
        resolvedLocalDurationMs,
        playbackMetadataDurationMs(),
    )
}

private fun Long.saturatingAdd(other: Long): Long =
    if (other > 0 && this > Long.MAX_VALUE - other) Long.MAX_VALUE else this + other

internal fun MediaItem.playbackRequestedStartPositionMs(): Long? {
    val extras = mediaMetadata.extras ?: return null
    if (!extras.containsKey(PlaybackMediaMetadata.EXTRA_REQUESTED_START_POSITION_MS)) return null
    return extras
        .getLong(PlaybackMediaMetadata.EXTRA_REQUESTED_START_POSITION_MS)
        .coerceAtLeast(0)
}

internal fun MediaItem.withPlaybackResolution(
    protocol: PlaybackStreamProtocol,
    sourceOffsetMs: Long,
    requestedStartPositionMs: Long? = null,
): MediaItem {
    val extras = Bundle(mediaMetadata.extras ?: Bundle()).apply {
        putString(PlaybackMediaMetadata.EXTRA_STREAM_PROTOCOL, protocol.name)
        putLong(PlaybackMediaMetadata.EXTRA_SOURCE_OFFSET_MS, sourceOffsetMs.coerceAtLeast(0))
        if (requestedStartPositionMs != null) {
            putLong(
                PlaybackMediaMetadata.EXTRA_REQUESTED_START_POSITION_MS,
                requestedStartPositionMs.coerceAtLeast(0),
            )
        } else {
            remove(PlaybackMediaMetadata.EXTRA_REQUESTED_START_POSITION_MS)
        }
    }
    return buildUpon()
        .setMediaMetadata(mediaMetadata.buildUpon().setExtras(extras).build())
        .build()
}

internal fun MediaItem.withoutPlaybackResolution(): MediaItem {
    val extras = Bundle(mediaMetadata.extras ?: Bundle()).apply {
        remove(PlaybackMediaMetadata.EXTRA_STREAM_PROTOCOL)
        remove(PlaybackMediaMetadata.EXTRA_SOURCE_OFFSET_MS)
        remove(PlaybackMediaMetadata.EXTRA_REQUESTED_START_POSITION_MS)
    }
    return buildUpon()
        .setMediaMetadata(mediaMetadata.buildUpon().setExtras(extras).build())
        .build()
}
