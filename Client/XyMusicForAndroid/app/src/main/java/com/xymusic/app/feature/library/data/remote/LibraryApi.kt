package com.xymusic.app.feature.library.data.remote

import retrofit2.Response
import retrofit2.http.Body
import retrofit2.http.DELETE
import retrofit2.http.GET
import retrofit2.http.Header
import retrofit2.http.PUT
import retrofit2.http.Path
import retrofit2.http.Query

interface LibraryApi {
    @GET("api/v1/library/favorites")
    suspend fun favorites(
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int,
        @Query("sort") sort: String,
    ): Response<FavoritePageDto>

    @PUT("api/v1/library/favorites/{trackId}")
    suspend fun addFavorite(@Path("trackId") trackId: String): Response<FavoriteItemDto>

    @DELETE("api/v1/library/favorites/{trackId}")
    suspend fun removeFavorite(@Path("trackId") trackId: String): Response<Unit>

    @GET("api/v1/library/history")
    suspend fun history(@Query("cursor") cursor: String?, @Query("limit") limit: Int): Response<HistoryPageDto>

    @PUT("api/v1/library/history/{trackId}")
    suspend fun recordPlayback(
        @Path("trackId") trackId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: RecordPlaybackRequestDto,
    ): Response<HistoryItemDto>
}
