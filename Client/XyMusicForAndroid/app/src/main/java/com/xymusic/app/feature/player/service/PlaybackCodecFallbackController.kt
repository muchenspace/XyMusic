package com.xymusic.app.feature.player.service

import androidx.media3.common.MediaItem
import androidx.media3.common.MimeTypes
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.Timeline
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlaybackException
import com.xymusic.app.feature.player.data.media.PlaybackMediaMetadata
import com.xymusic.app.feature.player.data.media.PlaybackMediaUri
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import java.util.UUID

@UnstableApi
internal class PlaybackCodecFallbackController(
    private val player: Player,
    private val grantRepository: PlaybackGrantRepository,
    private val onFallbackApplied: () -> Unit,
) : Player.Listener {
    private val attemptedQueueItemIds = mutableSetOf<String>()
    private var fallbackNotificationSent = false

    override fun onPlayerError(error: PlaybackException) {
        val target = fallbackTarget(error) ?: return
        if (!attemptedQueueItemIds.add(target.queueItemId)) return
        val fallbackAvailable =
            grantRepository.enableCompatibleCodecFallback(target.trackId) ||
                grantRepository.isCompatibleCodecFallbackEnabled(target.trackId)
        if (!fallbackAvailable) return

        player.seekTo(target.mediaItemIndex, target.positionMs)
        player.prepare()
        player.playWhenReady = target.playWhenReady
        if (!fallbackNotificationSent) {
            fallbackNotificationSent = true
            onFallbackApplied()
        }
    }

    override fun onTimelineChanged(timeline: Timeline, reason: Int) {
        attemptedQueueItemIds.retainAll(activeQueueItemIds())
    }

    fun resetForAccountChange() {
        attemptedQueueItemIds.clear()
        fallbackNotificationSent = false
    }

    private fun fallbackTarget(error: PlaybackException): CodecFallbackTarget? {
        if (!isFlacDecoderRendererError(error)) return null
        val mediaItemIndex =
            player.currentMediaItemIndex.takeIf { index -> index in 0 until player.mediaItemCount }
                ?: return null
        val mediaItem = player.getMediaItemAt(mediaItemIndex)
        val queueItemId = mediaItem.mediaId.takeIf(String::isNotBlank) ?: return null
        val trackId = mediaItem.playbackTrackId() ?: return null
        return CodecFallbackTarget(
            queueItemId = queueItemId,
            trackId = trackId,
            mediaItemIndex = mediaItemIndex,
            positionMs = player.currentPosition.coerceAtLeast(0),
            playWhenReady = player.playWhenReady,
        )
    }

    private fun activeQueueItemIds(): Set<String> = buildSet {
        repeat(player.mediaItemCount) { index ->
            player
                .getMediaItemAt(index)
                .mediaId
                .takeIf(String::isNotBlank)
                ?.let(::add)
        }
    }
}

@UnstableApi
internal fun isFlacDecoderRendererError(error: PlaybackException): Boolean {
    val exoError = error as? ExoPlaybackException ?: return false
    return exoError.type == ExoPlaybackException.TYPE_RENDERER &&
        exoError.errorCode in FLAC_FALLBACK_ERROR_CODES &&
        exoError.rendererFormat?.sampleMimeType == MimeTypes.AUDIO_FLAC
}

private fun MediaItem.playbackTrackId(): String? {
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

private data class CodecFallbackTarget(
    val queueItemId: String,
    val trackId: String,
    val mediaItemIndex: Int,
    val positionMs: Long,
    val playWhenReady: Boolean,
)

private val FLAC_FALLBACK_ERROR_CODES =
    setOf(
        PlaybackException.ERROR_CODE_DECODER_INIT_FAILED,
        PlaybackException.ERROR_CODE_DECODING_FAILED,
    )
