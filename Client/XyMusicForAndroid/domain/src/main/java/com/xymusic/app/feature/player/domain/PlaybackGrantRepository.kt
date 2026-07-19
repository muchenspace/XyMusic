package com.xymusic.app.feature.player.domain

import com.xymusic.app.feature.player.domain.model.PreferredQuality

interface PlaybackGrantRepository {
    suspend fun get(
        trackId: String,
        preferredQuality: PreferredQuality = PreferredQuality.AUTO,
        acceptedCodecs: List<String> = emptyList(),
        forceRefresh: Boolean = false,
    ): PlayerResult<PlaybackGrant>

    fun invalidate(trackId: String)

    fun enableCompatibleCodecFallback(trackId: String): Boolean = false

    fun isCompatibleCodecFallbackEnabled(trackId: String): Boolean = false

    fun clear()
}

class PlaybackGrant(
    val trackId: String,
    val variantId: String,
    val selectedQuality: PreferredQuality,
    val signedUrl: String,
    val expiresAtEpochMillis: Long,
    val mimeType: String,
    val codec: String,
    val container: String,
    val bitrate: Int,
    val sampleRate: Int?,
    val contentLength: Long,
    val checksumSha256: String?,
    val cacheKey: String,
) {
    override fun toString(): String = "PlaybackGrant(trackId=$trackId, variantId=$variantId, signedUrl=[REDACTED])"
}
