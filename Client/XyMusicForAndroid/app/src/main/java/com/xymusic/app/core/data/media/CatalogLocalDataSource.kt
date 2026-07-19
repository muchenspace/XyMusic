package com.xymusic.app.core.data.media

import androidx.paging.PagingSource
import com.xymusic.app.core.data.media.remote.AlbumDetailDto
import com.xymusic.app.core.data.media.remote.AlbumSummaryDto
import com.xymusic.app.core.data.media.remote.ArtistDetailDto
import com.xymusic.app.core.data.media.remote.ArtistSummaryDto
import com.xymusic.app.core.data.media.remote.TrackDetailDto
import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import com.xymusic.app.core.database.dao.CatalogDao
import com.xymusic.app.core.database.entity.ArtistEntity
import com.xymusic.app.core.database.model.AlbumReadModel
import com.xymusic.app.core.database.model.TrackDetailReadModel
import com.xymusic.app.core.database.model.TrackSummaryReadModel
import javax.inject.Inject
import kotlinx.coroutines.flow.Flow

interface CatalogLocalDataSource {
    fun pagedTracks(collectionKey: String): PagingSource<Int, TrackSummaryReadModel>

    fun pagedArtists(collectionKey: String): PagingSource<Int, ArtistEntity>

    fun pagedAlbums(collectionKey: String): PagingSource<Int, AlbumReadModel>

    fun observeTrack(trackId: String): Flow<TrackDetailReadModel?>

    fun observeArtist(artistId: String): Flow<ArtistEntity?>

    fun observeAlbum(albumId: String): Flow<AlbumReadModel?>

    suspend fun mergeTrackSummaries(items: List<TrackSummaryDto>, cachedAtEpochMs: Long)

    suspend fun mergeArtistSummaries(items: List<ArtistSummaryDto>, cachedAtEpochMs: Long)

    suspend fun mergeAlbumSummaries(items: List<AlbumSummaryDto>, cachedAtEpochMs: Long)

    suspend fun replaceTrack(detail: TrackDetailDto, cachedAtEpochMs: Long)

    suspend fun replaceArtist(detail: ArtistDetailDto, cachedAtEpochMs: Long)

    suspend fun replaceAlbum(detail: AlbumDetailDto, cachedAtEpochMs: Long)
}

class RoomCatalogLocalDataSource
@Inject
constructor(private val catalogDao: CatalogDao) : CatalogLocalDataSource {
    override fun pagedTracks(collectionKey: String): PagingSource<Int, TrackSummaryReadModel> =
        catalogDao.pagedTracks(collectionKey)

    override fun pagedArtists(collectionKey: String): PagingSource<Int, ArtistEntity> =
        catalogDao.pagedArtists(collectionKey)

    override fun pagedAlbums(collectionKey: String): PagingSource<Int, AlbumReadModel> =
        catalogDao.pagedAlbums(collectionKey)

    override fun observeTrack(trackId: String): Flow<TrackDetailReadModel?> = catalogDao.observeTrack(trackId)

    override fun observeArtist(artistId: String): Flow<ArtistEntity?> = catalogDao.observeArtist(artistId)

    override fun observeAlbum(albumId: String): Flow<AlbumReadModel?> = catalogDao.observeAlbum(albumId)

    override suspend fun mergeTrackSummaries(items: List<TrackSummaryDto>, cachedAtEpochMs: Long) {
        val writeModels =
            items
                .map { it.toWriteModel(cachedAtEpochMs) }
                .associateBy { it.track.id }
                .values
                .toList()
        catalogDao.mergeArtistReferences(
            writeModels
                .flatMap(TrackWriteModel::artistReferences)
                .associateBy { it.id }
                .values
                .toList(),
        )
        catalogDao.mergeAlbumReferences(
            writeModels
                .mapNotNull(TrackWriteModel::albumReference)
                .associateBy { it.id }
                .values
                .toList(),
        )
        catalogDao.replaceTrackMetadata(
            tracks = writeModels.map(TrackWriteModel::track),
            credits = writeModels.flatMap(TrackWriteModel::credits),
        )
    }

    override suspend fun mergeArtistSummaries(items: List<ArtistSummaryDto>, cachedAtEpochMs: Long) {
        catalogDao.mergeArtistSummaries(
            items
                .map { it.toEntity(cachedAtEpochMs) }
                .associateBy { it.id }
                .values
                .toList(),
        )
    }

    override suspend fun mergeAlbumSummaries(items: List<AlbumSummaryDto>, cachedAtEpochMs: Long) {
        val writeModels =
            items
                .map { it.toWriteModel(cachedAtEpochMs) }
                .associateBy { it.album.id }
                .values
                .toList()
        catalogDao.mergeArtistReferences(
            writeModels
                .flatMap(AlbumWriteModel::artistReferences)
                .associateBy { it.id }
                .values
                .toList(),
        )
        catalogDao.mergeAlbumSummaries(
            albums = writeModels.map(AlbumWriteModel::album),
            credits = writeModels.flatMap(AlbumWriteModel::credits),
        )
    }

    override suspend fun replaceTrack(detail: TrackDetailDto, cachedAtEpochMs: Long) {
        val item = detail.toWriteModel(cachedAtEpochMs)
        catalogDao.mergeArtistReferences(item.artistReferences)
        item.albumReference?.let { catalogDao.mergeAlbumReferences(listOf(it)) }
        catalogDao.replaceTrackMetadata(item.track, item.credits)
        catalogDao.replaceLyrics(item.track.id, requireNotNull(item.lyrics))
    }

    override suspend fun replaceArtist(detail: ArtistDetailDto, cachedAtEpochMs: Long) {
        catalogDao.upsertArtists(listOf(detail.toEntity(cachedAtEpochMs)))
    }

    override suspend fun replaceAlbum(detail: AlbumDetailDto, cachedAtEpochMs: Long) {
        val item = detail.toWriteModel(cachedAtEpochMs)
        catalogDao.mergeArtistReferences(item.artistReferences)
        catalogDao.replaceAlbum(item.album, item.credits)
    }
}
