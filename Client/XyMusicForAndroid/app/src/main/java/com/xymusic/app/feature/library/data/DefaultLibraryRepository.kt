package com.xymusic.app.feature.library.data

import androidx.paging.PagingData
import com.xymusic.app.core.common.IoDispatcher
import com.xymusic.app.core.data.media.CatalogLocalDataSource
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.dao.CatalogDao
import com.xymusic.app.core.database.dao.LibraryDao
import com.xymusic.app.core.database.dao.PendingSyncOperationDao
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.core.sync.PendingSyncScheduler
import com.xymusic.app.feature.library.data.remote.LibraryRemoteDataSource
import com.xymusic.app.feature.library.domain.LibraryRepository
import com.xymusic.app.feature.library.domain.LibraryResult
import com.xymusic.app.feature.library.domain.model.FavoriteSort
import com.xymusic.app.feature.library.domain.model.PlaybackHistoryItem
import com.xymusic.app.feature.library.domain.model.PlaybackProgressCommand
import java.time.Clock
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.flow.Flow
import kotlinx.serialization.json.Json

@Singleton
class DefaultLibraryRepository
@Inject
constructor(
    database: XyMusicDatabase,
    libraryDao: LibraryDao,
    catalogDao: CatalogDao,
    pendingDao: PendingSyncOperationDao,
    catalogLocal: CatalogLocalDataSource,
    remote: LibraryRemoteDataSource,
    sessionProvider: AppSessionProvider,
    sessionMutationCoordinator: SessionMutationCoordinator,
    serverRuntimeCoordinator: ServerRuntimeCoordinator,
    pendingSyncScheduler: PendingSyncScheduler,
    json: Json,
    clock: Clock,
    @IoDispatcher ioDispatcher: CoroutineDispatcher,
) : LibraryRepository {
    private val executionContext =
        LibraryRepositoryExecutionContext(
            sessionProvider = sessionProvider,
            sessionMutationCoordinator = sessionMutationCoordinator,
            serverRuntimeCoordinator = serverRuntimeCoordinator,
            ioDispatcher = ioDispatcher,
        )
    private val queries =
        LibraryQueries(
            libraryDao = libraryDao,
            sessionProvider = sessionProvider,
        )
    private val favoriteOperations =
        LibraryFavoriteOperations(
            database = database,
            libraryDao = libraryDao,
            catalogDao = catalogDao,
            pendingDao = pendingDao,
            catalogLocal = catalogLocal,
            remote = remote,
            executionContext = executionContext,
            pendingSyncScheduler = pendingSyncScheduler,
            json = json,
            clock = clock,
            mutationCoordinator = FavoriteMutationCoordinator(),
        )
    private val historyOperations =
        LibraryHistoryOperations(
            database = database,
            libraryDao = libraryDao,
            catalogDao = catalogDao,
            pendingDao = pendingDao,
            catalogLocal = catalogLocal,
            remote = remote,
            executionContext = executionContext,
            pendingSyncScheduler = pendingSyncScheduler,
            json = json,
            clock = clock,
        )

    override fun observeIsFavorite(trackId: String): Flow<Boolean> = queries.observeIsFavorite(trackId)

    override fun favoriteTracks(): Flow<PagingData<Track>> = queries.favoriteTracks()

    override fun playbackHistory(): Flow<PagingData<PlaybackHistoryItem>> = queries.playbackHistory()

    override suspend fun refreshFavorites(sort: FavoriteSort): LibraryResult<Unit> =
        favoriteOperations.refreshFavorites(sort)

    override suspend fun refreshHistory(): LibraryResult<Unit> = historyOperations.refreshHistory()

    override suspend fun setFavorite(trackId: String, favorite: Boolean): LibraryResult<Unit> =
        favoriteOperations.setFavorite(trackId, favorite)

    override suspend fun recordPlayback(command: PlaybackProgressCommand): LibraryResult<Unit> =
        historyOperations.recordPlayback(command)

    override suspend fun recordPlaybackForOwner(
        ownerUserId: String,
        command: PlaybackProgressCommand,
    ): LibraryResult<Unit> = historyOperations.recordPlaybackForOwner(ownerUserId, command)
}
