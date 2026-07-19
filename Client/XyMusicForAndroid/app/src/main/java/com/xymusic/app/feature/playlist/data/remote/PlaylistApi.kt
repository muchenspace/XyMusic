package com.xymusic.app.feature.playlist.data.remote

import kotlinx.serialization.json.JsonObject
import retrofit2.Response
import retrofit2.http.Body
import retrofit2.http.DELETE
import retrofit2.http.GET
import retrofit2.http.Header
import retrofit2.http.PATCH
import retrofit2.http.POST
import retrofit2.http.Path
import retrofit2.http.Query

interface PlaylistApi {
    @GET("api/v1/playlists")
    suspend fun playlists(
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int,
        @Query("sort") sort: String,
    ): Response<PlaylistPageDto>

    @POST("api/v1/playlists")
    suspend fun create(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: CreatePlaylistRequestDto,
    ): Response<PlaylistSummaryDto>

    @GET("api/v1/playlists/{id}")
    suspend fun playlist(
        @Path("id") playlistId: String,
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int,
    ): Response<PlaylistDetailDto>

    @PATCH("api/v1/playlists/{id}")
    suspend fun update(
        @Path("id") playlistId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: JsonObject,
    ): Response<PlaylistSummaryDto>

    @DELETE("api/v1/playlists/{id}")
    suspend fun delete(
        @Path("id") playlistId: String,
        @Query("expectedVersion") expectedVersion: Long,
        @Header("Idempotency-Key") idempotencyKey: String,
    ): Response<Unit>

    @POST("api/v1/playlists/{id}/tracks")
    suspend fun addTrack(
        @Path("id") playlistId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: AddPlaylistTrackRequestDto,
    ): Response<PlaylistEntryMutationDto>

    @DELETE("api/v1/playlists/{id}/tracks/{entryId}")
    suspend fun removeTrack(
        @Path("id") playlistId: String,
        @Path("entryId") entryId: String,
        @Query("expectedVersion") expectedVersion: Long,
        @Header("Idempotency-Key") idempotencyKey: String,
    ): Response<PlaylistMutationDto>

    @PATCH("api/v1/playlists/{id}/tracks/order")
    suspend fun reorder(
        @Path("id") playlistId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: ReorderPlaylistRequestDto,
    ): Response<PlaylistMutationDto>
}
