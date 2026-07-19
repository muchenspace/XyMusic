package com.xymusic.app.feature.settings.data.remote

import com.xymusic.app.core.data.media.remote.ArtworkDto
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject
import retrofit2.Response
import retrofit2.http.Body
import retrofit2.http.GET
import retrofit2.http.Header
import retrofit2.http.PATCH
import retrofit2.http.POST
import retrofit2.http.Path

interface ProfileApi {
    @GET("api/v1/users/me")
    suspend fun currentUser(): Response<CurrentUserDto>

    @PATCH("api/v1/users/me")
    suspend fun updateCurrentUser(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: JsonObject,
    ): Response<CurrentUserDto>

    @POST("api/v1/users/me/avatar/uploads")
    suspend fun createAvatarUpload(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: CreateAvatarUploadRequestDto,
    ): Response<AvatarUploadDto>

    @POST("api/v1/users/me/avatar/uploads/{id}/complete")
    suspend fun completeAvatarUpload(
        @Path("id") uploadId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: CompleteAvatarUploadRequestDto = CompleteAvatarUploadRequestDto(),
    ): Response<CurrentUserDto>

    @POST("api/v1/auth/logout-all")
    suspend fun logoutAllSessions(): Response<Unit>
}

@Serializable
data class CurrentUserDto(
    val id: String,
    val username: String,
    val displayName: String,
    val bio: String?,
    val avatar: ArtworkDto?,
    val role: String,
    val status: String,
    val version: Long,
    val createdAt: String,
    val updatedAt: String,
)

@Serializable
data class CreateAvatarUploadRequestDto(
    val fileName: String,
    val contentType: String,
    val sizeBytes: Int,
    val checksumSha256: String,
)

@Serializable
data class AvatarUploadDto(val id: String, val uploadUrl: String, val requiredHeaders: Map<String, String>)

@Serializable
data class CompleteAvatarUploadRequestDto(val observedEtag: String? = null)
