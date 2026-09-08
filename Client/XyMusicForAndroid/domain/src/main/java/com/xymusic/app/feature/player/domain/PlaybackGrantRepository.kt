package com.xymusic.app.feature.player.domain

interface PlaybackGrantRepository {
    suspend fun get(
        trackId: String,
        forceRefresh: Boolean = false,
    ): PlayerResult<PlaybackGrant>

    fun invalidate(trackId: String)

    fun clear()
}

class PlaybackGrant(
    val trackId: String,
    val streamUrl: String,
    val expiresAtEpochMillis: Long,
    val mimeType: String,
    val codec: String,
    val container: String,
    val bitrate: Int,
    val sampleRate: Int?,
    val contentLength: Long?,
    val durationMs: Long? = null,
) {
    override fun toString(): String = "PlaybackGrant(trackId=$trackId, streamUrl=[REDACTED])"
}
