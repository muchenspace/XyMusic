package com.xymusic.app.feature.player.data.remote

import kotlinx.serialization.Serializable
import okhttp3.ResponseBody
import retrofit2.Response
import retrofit2.http.Body
import retrofit2.http.Header
import retrofit2.http.POST
import retrofit2.http.PUT
import retrofit2.http.Path

interface PlaybackApi {
    @POST("api/v1/tracks/{id}/playback")
    suspend fun grant(@Path("id") trackId: String, @Body request: PlaybackRequestDto): Response<PlaybackGrantDto>

    @PUT("api/v1/library/history/{trackId}")
    suspend fun recordHistory(
        @Path("trackId") trackId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: RecordPlaybackRequestDto,
    ): Response<ResponseBody>
}

@Serializable
data class PlaybackRequestDto(val preferredQuality: String, val acceptedCodecs: List<String> = emptyList())

@Serializable
data class PlaybackGrantDto(
    val trackId: String,
    val variantId: String,
    val selectedQuality: String,
    val url: String,
    val expiresAt: String,
    val mimeType: String,
    val codec: String,
    val container: String,
    val bitrate: Int,
    val sampleRate: Int? = null,
    val contentLength: Long,
    val checksumSha256: String? = null,
    val cacheKey: String,
)

@Serializable
data class RecordPlaybackRequestDto(
    val playbackSessionId: String,
    val positionMs: Long,
    val occurredAt: String,
    val event: String,
)
