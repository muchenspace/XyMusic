package com.xymusic.app.feature.catalog.domain

import com.xymusic.app.core.model.media.Album
import com.xymusic.app.core.model.media.Artist
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.core.model.media.TrackDetail
import com.xymusic.app.domain.error.DomainError
import com.xymusic.app.domain.paging.PagedStream
import com.xymusic.app.feature.catalog.domain.model.AlbumQuery
import com.xymusic.app.feature.catalog.domain.model.ArtistQuery
import com.xymusic.app.feature.catalog.domain.model.TrackQuery
import kotlinx.coroutines.flow.Flow

interface CatalogRepository {
    fun pagedTracks(query: TrackQuery): PagedStream<Track>

    fun pagedArtists(query: ArtistQuery): PagedStream<Artist>

    fun pagedAlbums(query: AlbumQuery): PagedStream<Album>

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
