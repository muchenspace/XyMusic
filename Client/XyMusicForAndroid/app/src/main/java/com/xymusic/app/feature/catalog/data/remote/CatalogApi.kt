package com.xymusic.app.feature.catalog.data.remote

import com.xymusic.app.core.data.media.remote.AlbumDetailDto
import com.xymusic.app.core.data.media.remote.AlbumPageDto
import com.xymusic.app.core.data.media.remote.ArtistDetailDto
import com.xymusic.app.core.data.media.remote.ArtistPageDto
import com.xymusic.app.core.data.media.remote.RandomAlbumsResponseDto
import com.xymusic.app.core.data.media.remote.RandomCatalogRequestDto
import com.xymusic.app.core.data.media.remote.RandomTracksResponseDto
import com.xymusic.app.core.data.media.remote.TrackDetailDto
import com.xymusic.app.core.data.media.remote.TrackPageDto
import retrofit2.Response
import retrofit2.http.Body
import retrofit2.http.GET
import retrofit2.http.POST
import retrofit2.http.Path
import retrofit2.http.Query

interface CatalogApi {
    @GET("api/v1/tracks")
    suspend fun tracks(
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int,
        @Query("artistId") artistId: String?,
        @Query("albumId") albumId: String?,
        @Query("sort") sort: String,
    ): Response<TrackPageDto>

    @POST("api/v1/tracks/random")
    suspend fun randomTracks(@Body request: RandomCatalogRequestDto): Response<RandomTracksResponseDto>

    @GET("api/v1/tracks/{id}")
    suspend fun track(
        @Path("id") trackId: String,
        @Query("lyricPage") lyricPage: Int = 1,
        @Query("lyricPageSize") lyricPageSize: Int = 20,
    ): Response<TrackDetailDto>

    @GET("api/v1/artists")
    suspend fun artists(
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int,
        @Query("sort") sort: String,
    ): Response<ArtistPageDto>

    @GET("api/v1/artists/{id}")
    suspend fun artist(@Path("id") artistId: String): Response<ArtistDetailDto>

    @GET("api/v1/albums")
    suspend fun albums(
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int,
        @Query("artistId") artistId: String?,
        @Query("sort") sort: String,
    ): Response<AlbumPageDto>

    @POST("api/v1/albums/random")
    suspend fun randomAlbums(@Body request: RandomCatalogRequestDto): Response<RandomAlbumsResponseDto>

    @GET("api/v1/albums/{id}")
    suspend fun album(@Path("id") albumId: String): Response<AlbumDetailDto>
}
