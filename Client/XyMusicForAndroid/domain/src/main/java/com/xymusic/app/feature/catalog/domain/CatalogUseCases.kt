package com.xymusic.app.feature.catalog.domain

import com.xymusic.app.feature.catalog.domain.model.AlbumQuery
import com.xymusic.app.feature.catalog.domain.model.ArtistQuery
import com.xymusic.app.feature.catalog.domain.model.TrackQuery
import javax.inject.Inject

class CatalogUseCases
@Inject
constructor(private val repository: CatalogRepository) {
    fun tracks(query: TrackQuery = TrackQuery()) = repository.pagedTracks(query)

    fun artists(query: ArtistQuery = ArtistQuery()) = repository.pagedArtists(query)

    fun albums(query: AlbumQuery = AlbumQuery()) = repository.pagedAlbums(query)

    suspend fun randomAlbums(limit: Int) = repository.randomAlbums(limit)

    suspend fun randomTracks(limit: Int) = repository.randomTracks(limit)

    fun observeTrack(trackId: String) = repository.observeTrack(trackId)

    fun observeArtist(artistId: String) = repository.observeArtist(artistId)

    fun observeAlbum(albumId: String) = repository.observeAlbum(albumId)

    suspend fun refreshTrack(trackId: String) = repository.refreshTrack(trackId)

    suspend fun refreshArtist(artistId: String) = repository.refreshArtist(artistId)

    suspend fun refreshAlbum(albumId: String) = repository.refreshAlbum(albumId)
}
