package com.xymusic.app.feature.catalog.domain

import androidx.paging.PagingData
import com.xymusic.app.core.model.media.Album
import com.xymusic.app.core.model.media.Artist
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.core.model.media.TrackDetail
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.feature.catalog.domain.model.AlbumQuery
import com.xymusic.app.feature.catalog.domain.model.ArtistQuery
import com.xymusic.app.feature.catalog.domain.model.TrackQuery
import kotlinx.coroutines.flow.Flow

interface CatalogRepository {
    fun pagedTracks(query: TrackQuery): Flow<PagingData<Track>>

    fun pagedArtists(query: ArtistQuery): Flow<PagingData<Artist>>

    fun pagedAlbums(query: AlbumQuery): Flow<PagingData<Album>>

    suspend fun randomAlbums(limit: Int): CatalogResult<List<Album>>

    suspend fun randomTracks(limit: Int): CatalogResult<List<Track>>

    fun observeTrack(trackId: String): Flow<TrackDetail?>

    fun observeArtist(artistId: String): Flow<Artist?>

    fun observeAlbum(albumId: String): Flow<Album?>

    suspend fun refreshTrack(trackId: String): CatalogResult<Unit>

    suspend fun refreshArtist(artistId: String): CatalogResult<Unit>

    suspend fun refreshAlbum(albumId: String): CatalogResult<Unit>
}

sealed interface CatalogResult<out T> {
    data class Success<T>(val value: T) : CatalogResult<T>

    data class Failure(val error: DomainError) : CatalogResult<Nothing>
}
