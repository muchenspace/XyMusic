package com.xymusic.app.feature.catalog.data

import androidx.paging.ExperimentalPagingApi
import androidx.paging.Pager
import androidx.paging.PagingConfig
import androidx.paging.PagingData
import androidx.paging.map
import com.xymusic.app.core.data.media.CatalogLocalDataSource
import com.xymusic.app.core.data.media.CatalogRemoteMediator
import com.xymusic.app.core.data.media.remote.AlbumDetailDto
import com.xymusic.app.core.data.media.remote.AlbumSummaryDto
import com.xymusic.app.core.data.media.remote.ArtistDetailDto
import com.xymusic.app.core.data.media.remote.ArtistSummaryDto
import com.xymusic.app.core.data.media.remote.ArtworkDto
import com.xymusic.app.core.data.media.remote.CatalogProtocolException
import com.xymusic.app.core.data.media.remote.TrackDetailDto
import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import com.xymusic.app.core.data.media.toDomain
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.dao.CatalogRemoteKeyDao
import com.xymusic.app.core.database.model.CatalogItemType
import com.xymusic.app.core.model.media.Album
import com.xymusic.app.core.model.media.AlbumReference
import com.xymusic.app.core.model.media.Artist
import com.xymusic.app.core.model.media.ArtistReference
import com.xymusic.app.core.model.media.Artwork
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.core.model.media.TrackDetail
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.feature.catalog.data.remote.CatalogRemoteDataSource
import com.xymusic.app.feature.catalog.domain.CatalogRepository
import com.xymusic.app.feature.catalog.domain.CatalogResult
import com.xymusic.app.feature.catalog.domain.model.AlbumQuery
import com.xymusic.app.feature.catalog.domain.model.ArtistQuery
import com.xymusic.app.feature.catalog.domain.model.TrackQuery
import java.time.Clock
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

@Singleton
@OptIn(ExperimentalPagingApi::class)
class DefaultCatalogRepository
@Inject
constructor(
    private val database: XyMusicDatabase,
    private val remoteKeyDao: CatalogRemoteKeyDao,
    private val local: CatalogLocalDataSource,
    private val remote: CatalogRemoteDataSource,
    private val clock: Clock,
    private val refreshExecutor: CatalogRefreshExecutor,
    private val serverRuntimeCoordinator: ServerRuntimeCoordinator,
) : CatalogRepository {
    override fun pagedTracks(query: TrackQuery): Flow<PagingData<Track>> {
        val collectionKey = query.collectionKey()
        return Pager(
            config = pagingConfig(),
            remoteMediator =
            CatalogRemoteMediator(
                database = database,
                remoteKeyDao = remoteKeyDao,
                collectionKey = collectionKey,
                itemType = CatalogItemType.TRACK,
                clock = clock,
                serverRuntimeCoordinator = serverRuntimeCoordinator,
                itemId = TrackSummaryDto::id,
                loadPage = { cursor, limit -> remote.tracks(cursor, limit, query) },
                mergeItems = local::mergeTrackSummaries,
            ),
            pagingSourceFactory = { local.pagedTracks(collectionKey) },
        ).flow.map { pagingData -> pagingData.map { it.toDomain() } }
    }

    override fun pagedArtists(query: ArtistQuery): Flow<PagingData<Artist>> {
        val collectionKey = query.collectionKey()
        return Pager(
            config = pagingConfig(),
            remoteMediator =
            CatalogRemoteMediator(
                database = database,
                remoteKeyDao = remoteKeyDao,
                collectionKey = collectionKey,
                itemType = CatalogItemType.ARTIST,
                clock = clock,
                serverRuntimeCoordinator = serverRuntimeCoordinator,
                itemId = ArtistSummaryDto::id,
                loadPage = { cursor, limit -> remote.artists(cursor, limit, query) },
                mergeItems = local::mergeArtistSummaries,
            ),
            pagingSourceFactory = { local.pagedArtists(collectionKey) },
        ).flow.map { pagingData -> pagingData.map { it.toDomain() } }
    }

    override fun pagedAlbums(query: AlbumQuery): Flow<PagingData<Album>> {
        val collectionKey = query.collectionKey()
        return Pager(
            config = pagingConfig(),
            remoteMediator =
            CatalogRemoteMediator(
                database = database,
                remoteKeyDao = remoteKeyDao,
                collectionKey = collectionKey,
                itemType = CatalogItemType.ALBUM,
                clock = clock,
                serverRuntimeCoordinator = serverRuntimeCoordinator,
                itemId = AlbumSummaryDto::id,
                loadPage = { cursor, limit -> remote.albums(cursor, limit, query) },
                mergeItems = local::mergeAlbumSummaries,
            ),
            pagingSourceFactory = { local.pagedAlbums(collectionKey) },
        ).flow.map { pagingData -> pagingData.map { it.toDomain() } }
    }

    override suspend fun randomAlbums(limit: Int): CatalogResult<List<Album>> {
        requireRandomLimit(limit)
        return refreshExecutor.execute(
            request = {
                validateRandomResponse(
                    items = remote.randomAlbums(limit),
                    limit = limit,
                    itemId = AlbumSummaryDto::id,
                    itemLabel = "album",
                )
            },
            persist = { items, cachedAt ->
                local.mergeAlbumSummaries(items, cachedAt)
                items.map(AlbumSummaryDto::toRandomDomain)
            },
        )
    }

    override suspend fun randomTracks(limit: Int): CatalogResult<List<Track>> {
        requireRandomLimit(limit)
        return refreshExecutor.execute(
            request = {
                validateRandomResponse(
                    items = remote.randomTracks(limit),
                    limit = limit,
                    itemId = TrackSummaryDto::id,
                    itemLabel = "track",
                )
            },
            persist = { items, cachedAt ->
                local.mergeTrackSummaries(items, cachedAt)
                items.map(TrackSummaryDto::toRandomDomain)
            },
        )
    }

    override fun observeTrack(trackId: String): Flow<TrackDetail?> = local.observeTrack(trackId).map { it?.toDomain() }

    override fun observeArtist(artistId: String): Flow<Artist?> = local.observeArtist(artistId).map { it?.toDomain() }

    override fun observeAlbum(albumId: String): Flow<Album?> = local.observeAlbum(albumId).map { it?.toDomain() }

    override suspend fun refreshTrack(trackId: String): CatalogResult<Unit> = refreshExecutor.execute(
        request = {
            remote.track(trackId).also { detail ->
                if (detail.id != trackId) throw CatalogProtocolException("Track detail ID mismatch")
            }
        },
        persist = { detail: TrackDetailDto, cachedAt -> local.replaceTrack(detail, cachedAt) },
    )

    override suspend fun refreshArtist(artistId: String): CatalogResult<Unit> = refreshExecutor.execute(
        request = {
            remote.artist(artistId).also { detail ->
                if (detail.id != artistId) throw CatalogProtocolException("Artist detail ID mismatch")
            }
        },
        persist = { detail: ArtistDetailDto, cachedAt -> local.replaceArtist(detail, cachedAt) },
    )

    override suspend fun refreshAlbum(albumId: String): CatalogResult<Unit> = refreshExecutor.execute(
        request = {
            remote.album(albumId).also { detail ->
                if (detail.id != albumId) throw CatalogProtocolException("Album detail ID mismatch")
            }
        },
        persist = { detail: AlbumDetailDto, cachedAt -> local.replaceAlbum(detail, cachedAt) },
    )

    private fun pagingConfig(): PagingConfig = PagingConfig(
        pageSize = PAGE_SIZE,
        initialLoadSize = INITIAL_LOAD_SIZE,
        prefetchDistance = PREFETCH_DISTANCE,
        enablePlaceholders = false,
    )

    private companion object {
        const val PAGE_SIZE = 20
        const val INITIAL_LOAD_SIZE = 40
        const val PREFETCH_DISTANCE = 6
    }
}

private fun requireRandomLimit(limit: Int) {
    require(limit in MIN_RANDOM_LIMIT..MAX_RANDOM_LIMIT) {
        "Random catalog limit must be between $MIN_RANDOM_LIMIT and $MAX_RANDOM_LIMIT"
    }
}

private fun <T> validateRandomResponse(items: List<T>, limit: Int, itemId: (T) -> String, itemLabel: String): List<T> {
    if (items.size > limit) {
        throw CatalogProtocolException("Random $itemLabel response exceeds the requested limit")
    }
    if (items.map(itemId).distinct().size != items.size) {
        throw CatalogProtocolException("Random $itemLabel response contains duplicate IDs")
    }
    return items
}

private fun AlbumSummaryDto.toRandomDomain(): Album = Album(
    id = id,
    title = title,
    artists = artists.map { artist -> ArtistReference(artist.id, artist.name) },
    cover = cover.toRandomDomain(),
    releaseDateEpochMillis =
    releaseDate?.let { date ->
        LocalDate
            .parse(date)
            .atStartOfDay()
            .toInstant(ZoneOffset.UTC)
            .toEpochMilli()
    },
    trackCount = trackCount,
    description = null,
)

private fun TrackSummaryDto.toRandomDomain(): Track = Track(
    id = id,
    title = title,
    artists = artists.map { artist -> ArtistReference(artist.id, artist.name) },
    album = album?.let { item -> AlbumReference(item.id, item.title) },
    artwork = artwork.toRandomDomain(),
    durationMs = durationMs,
    trackNumber = trackNumber,
    discNumber = discNumber,
    publishedAtEpochMillis = Instant.parse(publishedAt).toEpochMilli(),
)

private fun ArtworkDto?.toRandomDomain(): Artwork? = this?.let { artwork ->
    Artwork(
        assetId = artwork.assetId,
        url = artwork.url,
        cacheKey = artwork.cacheKey,
        mimeType = artwork.mimeType,
        expiresAtEpochMillis = artwork.expiresAt?.let(Instant::parse)?.toEpochMilli(),
        width = artwork.width,
        height = artwork.height,
    )
}

private const val MIN_RANDOM_LIMIT = 1
private const val MAX_RANDOM_LIMIT = 50
