package com.xymusic.app.feature.search.data

import androidx.room.withTransaction
import com.xymusic.app.core.data.media.CatalogLocalDataSource
import com.xymusic.app.core.data.media.toDomain
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.dao.CatalogDao
import com.xymusic.app.core.database.dao.CatalogRemoteKeyDao
import com.xymusic.app.core.database.entity.CatalogRemoteKeyEntity
import com.xymusic.app.core.database.model.CatalogItemType
import com.xymusic.app.core.network.ServerGeneration
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.feature.search.data.remote.SearchOverviewRemote
import com.xymusic.app.feature.search.domain.model.SearchOverview
import com.xymusic.app.feature.search.domain.model.SearchQuery
import java.time.Clock
import javax.inject.Inject
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.emitAll
import kotlinx.coroutines.flow.flow

interface SearchOverviewStore {
    fun observe(query: SearchQuery): Flow<SearchOverview?>

    suspend fun replace(query: SearchQuery, overview: SearchOverviewRemote)

    suspend fun replaceIfCurrent(
        query: SearchQuery,
        overview: SearchOverviewRemote,
        serverGeneration: ServerGeneration,
    ) = replace(query, overview)

    fun clearMemory() = Unit
}

class RoomSearchOverviewStore
@Inject
constructor(
    private val database: XyMusicDatabase,
    private val catalogDao: CatalogDao,
    private val remoteKeyDao: CatalogRemoteKeyDao,
    private val catalogLocal: CatalogLocalDataSource,
    private val clock: Clock,
    private val serverRuntimeCoordinator: ServerRuntimeCoordinator,
) : SearchOverviewStore {
    private val refreshedOverviewKeys = MutableStateFlow<Set<String>>(emptySet())

    override fun observe(query: SearchQuery): Flow<SearchOverview?> = flow {
        val overviewKey = query.searchCollectionKey()
        val hasPersistedItems = remoteKeyDao.searchCollectionLastUsedAt(overviewKey) != null
        if (hasPersistedItems) {
            refreshedOverviewKeys.value = rememberRecentKey(refreshedOverviewKeys.value, overviewKey)
        }
        emitAll(
            combine(
                catalogDao.observeSearchTrackOverview(query.searchCollectionKey()),
                catalogDao.observeSearchArtistOverview(query.searchCollectionKey()),
                catalogDao.observeSearchAlbumOverview(query.searchCollectionKey()),
                refreshedOverviewKeys,
            ) { tracks, artists, albums, refreshedKeys ->
                if (overviewKey !in refreshedKeys) return@combine null
                SearchOverview(
                    query = query,
                    tracks = tracks.map { it.toDomain() },
                    artists = artists.map { it.toDomain() },
                    albums = albums.map { it.toDomain() },
                )
            },
        )
    }

    override suspend fun replace(query: SearchQuery, overview: SearchOverviewRemote) {
        replaceIfCurrent(query, overview, serverRuntimeCoordinator.captureGeneration())
    }

    override suspend fun replaceIfCurrent(
        query: SearchQuery,
        overview: SearchOverviewRemote,
        serverGeneration: ServerGeneration,
    ) {
        val now = clock.millis()
        database.withTransaction {
            serverRuntimeCoordinator.requireCurrent(serverGeneration)
            catalogLocal.mergeTrackSummaries(overview.tracks, now)
            catalogLocal.mergeArtistSummaries(overview.artists, now)
            catalogLocal.mergeAlbumSummaries(overview.albums, now)
            replaceKeys(
                query.searchCollectionKey(),
                CatalogItemType.TRACK,
                overview.tracks.map { it.id },
                now,
            )
            replaceKeys(
                query.searchCollectionKey(),
                CatalogItemType.ARTIST,
                overview.artists.map { it.id },
                now,
            )
            replaceKeys(
                query.searchCollectionKey(),
                CatalogItemType.ALBUM,
                overview.albums.map { it.id },
                now,
            )
            remoteKeyDao.pruneSearchCollections(
                expireBeforeEpochMs = (now - SEARCH_CACHE_TTL_MS).coerceAtLeast(0L),
            )
        }
        serverRuntimeCoordinator.requireCurrent(serverGeneration)
        refreshedOverviewKeys.value =
            rememberRecentKey(
                refreshedOverviewKeys.value,
                query.searchCollectionKey(),
            )
    }

    override fun clearMemory() {
        refreshedOverviewKeys.value = emptySet()
    }

    private suspend fun replaceKeys(
        collectionKey: String,
        itemType: CatalogItemType,
        itemIds: List<String>,
        now: Long,
    ) {
        val keys =
            itemIds.mapIndexed { index, itemId ->
                CatalogRemoteKeyEntity(
                    collectionKey = collectionKey,
                    itemType = itemType,
                    itemId = itemId,
                    position = index.toLong(),
                    previousCursor = null,
                    nextCursor = null,
                    refreshedAtEpochMs = now,
                )
            }
        remoteKeyDao.replace(collectionKey, itemType, keys)
        if (keys.isNotEmpty()) remoteKeyDao.markSearchCollectionUsed(collectionKey, now)
    }

    private companion object {
        const val SEARCH_CACHE_TTL_MS = 7L * 24 * 60 * 60 * 1_000
    }
}

internal fun rememberRecentKey(keys: Set<String>, key: String, maximumSize: Int = 100): Set<String> {
    require(maximumSize > 0)
    if (key in keys && keys.size <= maximumSize) return keys
    return buildSet {
        keys
            .filterNot { it == key }
            .takeLast(maximumSize - 1)
            .forEach(::add)
        add(key)
    }
}
