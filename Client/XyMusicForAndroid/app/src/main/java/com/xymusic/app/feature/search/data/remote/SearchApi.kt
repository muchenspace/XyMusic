package com.xymusic.app.feature.search.data.remote

import com.xymusic.app.core.data.media.remote.AlbumPageDto
import com.xymusic.app.core.data.media.remote.ArtistPageDto
import com.xymusic.app.core.data.media.remote.TrackPageDto
import kotlinx.serialization.Serializable
import retrofit2.Response
import retrofit2.http.GET
import retrofit2.http.Query

interface SearchApi {
    @GET("api/v1/search")
    suspend fun search(
        @Query("q") query: String,
        @Query("scope") scope: String,
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int,
    ): Response<CatalogSearchResponseDto>
}

@Serializable
data class CatalogSearchResponseDto(
    val query: String,
    val scope: String,
    val tracks: TrackPageDto? = null,
    val artists: ArtistPageDto? = null,
    val albums: AlbumPageDto? = null,
)
