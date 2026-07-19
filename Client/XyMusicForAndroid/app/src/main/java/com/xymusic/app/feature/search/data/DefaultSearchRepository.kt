package com.xymusic.app.feature.search.data

import androidx.paging.ExperimentalPagingApi
import androidx.paging.Pager
import androidx.paging.PagingConfig
import androidx.paging.PagingData
import androidx.paging.map
import com.xymusic.app.core.data.media.CatalogLocalDataSource
import com.xymusic.app.core.data.media.remote.AlbumSummaryDto
import com.xymusic.app.core.data.media.remote.ArtistSummaryDto
import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import com.xymusic.app.core.data.media.toDomain
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.dao.CatalogRemoteKeyDao
import com.xymusic.app.core.database.dao.SearchHistoryDao
import com.xymusic.app.core.database.entity.SearchHistoryEntity
import com.xymusic.app.core.database.model.CatalogItemType
import com.xymusic.app.core.database.model.SearchScope as DatabaseSearchScope
import com.xymusic.app.core.model.media.Album
import com.xymusic.app.core.model.media.Artist
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.network.model.ProblemCode
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.feature.search.data.remote.SearchRemoteDataSource
import com.xymusic.app.feature.search.domain.SearchRepository
import com.xymusic.app.feature.search.domain.SearchResult
import com.xymusic.app.feature.search.domain.model.SearchHistoryItem
import com.xymusic.app.feature.search.domain.model.SearchOverview
import com.xymusic.app.feature.search.domain.model.SearchQuery
import com.xymusic.app.feature.search.domain.model.SearchScope
import java.time.Clock
import java.util.concurrent.CancellationException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map

@Singleton
@OptIn(ExperimentalPagingApi::class, ExperimentalCoroutinesApi::class)
class DefaultSearchRepository
@Inject
constructor(
    private val database: XyMusicDatabase,
    private val remoteKeyDao: CatalogRemoteKeyDao,
    private val catalogLocal: CatalogLocalDataSource,
    private val remote: SearchRemoteDataSource,
    private val overviewStore: SearchOverviewStore,
    private val overviewRefresher: SearchOverviewRefresher,
    private val searchHistoryDao: SearchHistoryDao,
    private val sessionProvider: AppSessionProvider,
    private val sessionMutationCoordinator: SessionMutationCoordinator,
    private val clock: Clock,
    private val serverRuntimeCoordinator: ServerRuntimeCoordinator,
) : SearchRepository {
    override fun observeOverview(query: SearchQuery): Flow<SearchOverview?> = overviewStore.observe(query)

    override suspend fun refreshOverview(query: SearchQuery): SearchResult<Unit> = overviewRefresher.refresh(query)

    override fun pagedTracks(query: SearchQuery): Flow<PagingData<Track>> {
        val collectionKey = query.searchCollectionKey()
        return Pager(
            config = pagingConfig(),
            remoteMediator =
            SearchRemoteMediator(
                database = database,
                remoteKeyDao = remoteKeyDao,
                collectionKey = collectionKey,
                itemType = CatalogItemType.TRACK,
                clock = clock,
                serverRuntimeCoordinator = serverRuntimeCoordinator,
                itemId = TrackSummaryDto::id,
                loadPage = { cursor -> remote.tracks(cursor, query) },
                mergeItems = catalogLocal::mergeTrackSummaries,
            ),
            pagingSourceFactory = { catalogLocal.pagedTracks(collectionKey) },
        ).flow.map { page -> page.map { it.toDomain() } }
    }

    override fun pagedArtists(query: SearchQuery): Flow<PagingData<Artist>> {
        val collectionKey = query.searchCollectionKey()
        return Pager(
            config = pagingConfig(),
            remoteMediator =
            SearchRemoteMediator(
                database = database,
                remoteKeyDao = remoteKeyDao,
                collectionKey = collectionKey,
                itemType = CatalogItemType.ARTIST,
                clock = clock,
                serverRuntimeCoordinator = serverRuntimeCoordinator,
                itemId = ArtistSummaryDto::id,
                loadPage = { cursor -> remote.artists(cursor, query) },
                mergeItems = catalogLocal::mergeArtistSummaries,
            ),
            pagingSourceFactory = { catalogLocal.pagedArtists(collectionKey) },
        ).flow.map { page -> page.map { it.toDomain() } }
    }

    override fun pagedAlbums(query: SearchQuery): Flow<PagingData<Album>> {
        val collectionKey = query.searchCollectionKey()
        return Pager(
            config = pagingConfig(),
            remoteMediator =
            SearchRemoteMediator(
                database = database,
                remoteKeyDao = remoteKeyDao,
                collectionKey = collectionKey,
                itemType = CatalogItemType.ALBUM,
                clock = clock,
                serverRuntimeCoordinator = serverRuntimeCoordinator,
                itemId = AlbumSummaryDto::id,
                loadPage = { cursor -> remote.albums(cursor, query) },
                mergeItems = catalogLocal::mergeAlbumSummaries,
            ),
            pagingSourceFactory = { catalogLocal.pagedAlbums(collectionKey) },
        ).flow.map { page -> page.map { it.toDomain() } }
    }

    override fun observeHistory(): Flow<List<SearchHistoryItem>> = sessionProvider.sessionState.flatMapLatest { state ->
        val ownerUserId = (state as? AppSessionState.SignedIn)?.userId
        if (ownerUserId == null) {
            flowOf(emptyList())
        } else {
            searchHistoryDao.observe(ownerUserId).map { entries ->
                entries.mapNotNull { entry ->
                    runCatching {
                        SearchHistoryItem(
                            query = SearchQuery.from(entry.query),
                            scope = SearchScope.valueOf(entry.scope.name),
                            searchedAtEpochMillis = entry.searchedAtEpochMs,
                        )
                    }.getOrNull()
                }
            }
        }
    }

    override suspend fun record(query: SearchQuery, scope: SearchScope): SearchResult<Unit> =
        mutateHistory { ownerUserId ->
            searchHistoryDao.record(
                SearchHistoryEntity(
                    ownerUserId = ownerUserId,
                    normalizedQuery = query.normalizedValue,
                    scope = DatabaseSearchScope.valueOf(scope.name),
                    query = query.value,
                    searchedAtEpochMs = clock.millis(),
                ),
            )
        }

    override suspend fun delete(query: SearchQuery, scope: SearchScope): SearchResult<Unit> =
        mutateHistory { ownerUserId ->
            searchHistoryDao.delete(
                ownerUserId = ownerUserId,
                normalizedQuery = query.normalizedValue,
                scope = DatabaseSearchScope.valueOf(scope.name),
            )
        }

    override suspend fun clearHistory(): SearchResult<Unit> = mutateHistory { ownerUserId ->
        searchHistoryDao.clear(ownerUserId)
    }

    private suspend fun mutateHistory(mutation: suspend (ownerUserId: String) -> Unit): SearchResult<Unit> {
        val ownerUserId = activeOwner() ?: return signedOutFailure()
        return try {
            sessionMutationCoordinator.mutate {
                if (activeOwner() != ownerUserId) throw OwnerChangedException
                mutation(ownerUserId)
            }
            SearchResult.Success(Unit)
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: OwnerChangedException) {
            signedOutFailure()
        } catch (_: Exception) {
            localFailure()
        }
    }

    private fun activeOwner(): String? = (sessionProvider.sessionState.value as? AppSessionState.SignedIn)?.userId

    private fun pagingConfig(): PagingConfig = PagingConfig(
        pageSize = SPECIFIC_SCOPE_LIMIT,
        initialLoadSize = SPECIFIC_SCOPE_LIMIT,
        prefetchDistance = 5,
        enablePlaceholders = false,
    )

    private fun signedOutFailure(): SearchResult.Failure = SearchResult.Failure(
        DomainError.Authentication(
            detail = "Authentication is required",
            traceId = null,
            reason = ProblemCode.AuthenticationRequired,
        ),
    )

    private fun localFailure(): SearchResult.Failure = SearchResult.Failure(
        DomainError.Protocol("Unable to update search history", null, null),
    )

    private companion object {
        const val SPECIFIC_SCOPE_LIMIT = 20
    }

    private object OwnerChangedException : IllegalStateException()
}
