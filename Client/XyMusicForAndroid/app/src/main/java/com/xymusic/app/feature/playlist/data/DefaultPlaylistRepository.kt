package com.xymusic.app.feature.playlist.data

import com.xymusic.app.core.common.IoDispatcher
import com.xymusic.app.core.data.media.CatalogLocalDataSource
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.dao.CatalogDao
import com.xymusic.app.core.database.dao.PlaylistDao
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.feature.playlist.data.remote.PlaylistRemoteDataSource
import com.xymusic.app.feature.playlist.domain.PlaylistRepository
import com.xymusic.app.feature.playlist.domain.PlaylistResult
import com.xymusic.app.feature.playlist.domain.model.AddPlaylistTrackCommand
import com.xymusic.app.feature.playlist.domain.model.CreatePlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.PlaylistDetail
import com.xymusic.app.feature.playlist.domain.model.PlaylistDetailPage
import com.xymusic.app.feature.playlist.domain.model.PlaylistSort
import com.xymusic.app.feature.playlist.domain.model.PlaylistSummary
import com.xymusic.app.feature.playlist.domain.model.RemovePlaylistTrackCommand
import com.xymusic.app.feature.playlist.domain.model.ReorderPlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.UpdatePlaylistCommand
import java.time.Clock
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.flow.Flow

@Singleton
class DefaultPlaylistRepository
@Inject
constructor(
    database: XyMusicDatabase,
    playlistDao: PlaylistDao,
    catalogDao: CatalogDao,
    catalogLocal: CatalogLocalDataSource,
    remote: PlaylistRemoteDataSource,
    sessionProvider: AppSessionProvider,
    sessionMutationCoordinator: SessionMutationCoordinator,
    serverRuntimeCoordinator: ServerRuntimeCoordinator,
    clock: Clock,
    @IoDispatcher ioDispatcher: CoroutineDispatcher,
) : PlaylistRepository {
    private val executionContext =
        PlaylistRepositoryExecutionContext(
            sessionProvider = sessionProvider,
            sessionMutationCoordinator = sessionMutationCoordinator,
            serverRuntimeCoordinator = serverRuntimeCoordinator,
            ioDispatcher = ioDispatcher,
        )

    private val localStore =
        PlaylistLocalStore(
            database = database,
            playlistDao = playlistDao,
            catalogDao = catalogDao,
            catalogLocal = catalogLocal,
            clock = clock,
            executionContext = executionContext,
        )

    private val queries =
        PlaylistRepositoryQueries(
            playlistDao = playlistDao,
            catalogDao = catalogDao,
            sessionProvider = sessionProvider,
        )

    private val refreshOperations =
        PlaylistRefreshOperations(
            database = database,
            playlistDao = playlistDao,
            remote = remote,
            executionContext = executionContext,
            localStore = localStore,
        )

    private val mutationOperations =
        PlaylistMutationOperations(
            playlistDao = playlistDao,
            remote = remote,
            executionContext = executionContext,
            localStore = localStore,
        )

    override fun observePlaylists(): Flow<List<PlaylistSummary>> = queries.observePlaylists()

    override fun observePlaylist(playlistId: String): Flow<PlaylistDetail?> = queries.observePlaylist(playlistId)

    override suspend fun refreshPlaylists(sort: PlaylistSort): PlaylistResult<Unit> =
        refreshOperations.refreshPlaylists(sort)

    override suspend fun refreshPlaylist(playlistId: String): PlaylistResult<Unit> =
        refreshOperations.refreshPlaylist(playlistId)

    override suspend fun loadPlaylistPage(playlistId: String, cursor: String?): PlaylistResult<PlaylistDetailPage> =
        refreshOperations.loadPlaylistPage(playlistId, cursor)

    override suspend fun create(command: CreatePlaylistCommand): PlaylistResult<PlaylistSummary> =
        refreshOperations.create(command)

    override suspend fun update(command: UpdatePlaylistCommand): PlaylistResult<PlaylistSummary> =
        mutationOperations.update(command)

    override suspend fun delete(playlistId: String, expectedVersion: Long): PlaylistResult<Unit> =
        mutationOperations.delete(playlistId, expectedVersion)

    override suspend fun addTrack(command: AddPlaylistTrackCommand): PlaylistResult<Unit> =
        mutationOperations.addTrack(command)

    override suspend fun removeTrack(command: RemovePlaylistTrackCommand): PlaylistResult<Unit> =
        mutationOperations.removeTrack(command)

    override suspend fun reorder(command: ReorderPlaylistCommand): PlaylistResult<Unit> =
        mutationOperations.reorder(command)
}
