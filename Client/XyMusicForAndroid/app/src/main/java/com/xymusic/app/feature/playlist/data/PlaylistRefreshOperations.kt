package com.xymusic.app.feature.playlist.data

import androidx.room.withTransaction
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.dao.PlaylistDao
import com.xymusic.app.core.database.entity.PlaylistEntity
import com.xymusic.app.core.network.ServerGeneration
import com.xymusic.app.feature.playlist.data.remote.CreatePlaylistRequestDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistDetailDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistProtocolException
import com.xymusic.app.feature.playlist.data.remote.PlaylistRemoteDataSource
import com.xymusic.app.feature.playlist.domain.PlaylistResult
import com.xymusic.app.feature.playlist.domain.model.CreatePlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.PlaylistDetailPage
import com.xymusic.app.feature.playlist.domain.model.PlaylistSort
import com.xymusic.app.feature.playlist.domain.model.PlaylistSummary
import java.util.HashSet
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap

internal class PlaylistRefreshOperations(
    private val database: XyMusicDatabase,
    private val playlistDao: PlaylistDao,
    private val remote: PlaylistRemoteDataSource,
    private val executionContext: PlaylistRepositoryExecutionContext,
    private val localStore: PlaylistLocalStore,
) {
    private val pageSessions = ConcurrentHashMap<String, PlaylistPageSession>()
    private val pageSessionsLock = Any()

    suspend fun refreshPlaylists(sort: PlaylistSort): PlaylistResult<Unit> = executionContext.ioCall {
        val serverGeneration = executionContext.captureServerGeneration()
        val owner = executionContext.requireOwner()
        val localPlaylistsAtRefreshStart =
            playlistDao
                .playlists(owner)
                .associateBy(PlaylistEntity::id)
        val remoteItems = remote.allPlaylists(sort.name)
        require(remoteItems.all { it.owner.id == owner }) {
            "Playlist list contains another owner"
        }
        val remoteIds = HashSet<String>(remoteItems.size)
        remoteItems.forEach { item ->
            require(remoteIds.add(item.id)) { "Playlist list contains duplicate IDs" }
        }
        val remoteEntities = remoteItems.map { item -> item.toEntity(owner) }
        executionContext.withActiveOwner(owner, serverGeneration) {
            database.withTransaction {
                val currentPlaylists =
                    playlistDao
                        .playlists(owner)
                        .associateBy(PlaylistEntity::id)
                val safeRemoteEntities =
                    remoteEntities.filter { remoteEntity ->
                        val current = currentPlaylists[remoteEntity.id]
                        val disappearedDuringRefresh =
                            remoteEntity.id in localPlaylistsAtRefreshStart && current == null
                        !disappearedDuringRefresh &&
                            (current == null || current.version <= remoteEntity.version)
                    }
                if (safeRemoteEntities.isNotEmpty()) {
                    safeRemoteEntities
                        .asSequence()
                        .filter { remoteEntity ->
                            currentPlaylists[remoteEntity.id]?.version?.let { cachedVersion ->
                                cachedVersion != remoteEntity.version
                            } == true
                        }.map(PlaylistEntity::id)
                        .chunked(SQLITE_SAFE_BATCH_SIZE)
                        .forEach { playlistIds ->
                            playlistDao.deleteEntriesForPlaylists(owner, playlistIds)
                        }
                    playlistDao.upsertPlaylists(safeRemoteEntities)
                }
                localPlaylistsAtRefreshStart.keys
                    .asSequence()
                    .filter { playlistId ->
                        playlistId !in remoteIds &&
                            currentPlaylists[playlistId] == localPlaylistsAtRefreshStart[playlistId]
                    }.chunked(SQLITE_SAFE_BATCH_SIZE)
                    .forEach { playlistIds -> playlistDao.deletePlaylists(owner, playlistIds) }
            }
        }
        PlaylistResult.Success(Unit)
    }

    suspend fun refreshPlaylist(playlistId: String): PlaylistResult<Unit> =
        executionContext.serializePlaylistMutation(playlistId) {
            val serverGeneration = executionContext.captureServerGeneration()
            val owner = executionContext.requireOwner()
            val detail =
                remote.playlistProgressively(playlistId) {
                    // Do not expose incomplete data.
                }
            if (detail.nextCursor != null) {
                return@serializePlaylistMutation protocolFailure("Playlist detail is incomplete")
            }
            localStore.persistDetail(owner, detail, serverGeneration)
            PlaylistResult.Success(Unit)
        }

    suspend fun loadPlaylistPage(playlistId: String, cursor: String?): PlaylistResult<PlaylistDetailPage> =
        executionContext.serializePlaylistMutation(playlistId) {
            val pageLoad = createPageLoad(playlistId, cursor)
            val page = remote.playlistPage(playlistId, cursor, PLAYLIST_PAGE_LIMIT)
            validatePageOwner(page, pageLoad)
            mergeAndPersistPage(playlistId, cursor, page, pageLoad)
        }

    suspend fun create(command: CreatePlaylistCommand): PlaylistResult<PlaylistSummary> = executionContext.ioCall {
        val serverGeneration = executionContext.captureServerGeneration()
        val owner = executionContext.requireOwner()
        val result =
            remote.create(
                UUID.randomUUID().toString(),
                CreatePlaylistRequestDto(
                    command.name,
                    command.description,
                    command.visibility.name,
                ),
            )
        val entity = result.toEntity(owner)
        executionContext.withActiveOwner(owner, serverGeneration) {
            playlistDao.upsertPlaylist(entity)
        }
        PlaylistResult.Success(entity.toDomain())
    }

    private suspend fun createPageLoad(playlistId: String, cursor: String?): PlaylistPageLoad {
        val serverGeneration = executionContext.captureServerGeneration()
        val owner = executionContext.requireOwner()
        val sessionKey = "$owner:$playlistId"
        val pageLoad =
            PlaylistPageLoad(
                owner = owner,
                serverGeneration = serverGeneration,
                sessionKey = sessionKey,
                activeSession = activePageSession(cursor, sessionKey),
            )
        validateActivePageSession(pageLoad, playlistId)
        return pageLoad
    }

    private fun activePageSession(cursor: String?, sessionKey: String): PlaylistPageSession? {
        if (cursor == null) return null
        return pageSessions[sessionKey]
            ?: throw PlaylistProtocolException("Playlist continuation page has no active session")
    }

    private suspend fun validateActivePageSession(pageLoad: PlaylistPageLoad, playlistId: String) {
        val activeSession = pageLoad.activeSession ?: return
        if (activeSession.serverGeneration != pageLoad.serverGeneration) {
            pageSessions.remove(pageLoad.sessionKey)
            throw PlaylistProtocolException("Playlist continuation belongs to a stale server session")
        }
        val cachedVersion = playlistDao.playlist(pageLoad.owner, playlistId)?.version
        if (cachedVersion != null && cachedVersion != activeSession.accumulator.resourceVersion) {
            pageSessions.remove(pageLoad.sessionKey)
            throw PlaylistProtocolException("Playlist changed while loading continuation pages")
        }
    }

    private fun validatePageOwner(page: PlaylistDetailDto, pageLoad: PlaylistPageLoad) {
        if (page.owner.id != pageLoad.owner) {
            pageSessions.remove(pageLoad.sessionKey)
            throw PlaylistProtocolException("Playlist belongs to another owner")
        }
    }

    private suspend fun mergeAndPersistPage(
        playlistId: String,
        cursor: String?,
        page: PlaylistDetailDto,
        pageLoad: PlaylistPageLoad,
    ): PlaylistResult<PlaylistDetailPage> {
        try {
            val (accumulator, mergeResult) =
                if (cursor == null) {
                    PlaylistPageAccumulator.start(playlistId, page)
                } else {
                    val current = requireNotNull(pageLoad.activeSession).accumulator
                    current to current.append(cursor, page)
                }
            persistPageLoad(pageLoad, accumulator, mergeResult)
            return PlaylistResult.Success(mergeResult.page.toDomainPage(pageLoad.owner))
        } catch (failure: Exception) {
            pageSessions.remove(pageLoad.sessionKey)
            throw failure
        }
    }

    private suspend fun persistPageLoad(
        pageLoad: PlaylistPageLoad,
        accumulator: PlaylistPageAccumulator?,
        mergeResult: PlaylistPageMergeResult,
    ) {
        val completeDetail = mergeResult.completeDetail
        if (completeDetail == null) {
            localStore.persistPagePreview(pageLoad.owner, mergeResult.page, pageLoad.serverGeneration)
            storePageSession(
                pageLoad.sessionKey,
                PlaylistPageSession(
                    serverGeneration = pageLoad.serverGeneration,
                    accumulator = requireNotNull(accumulator),
                ),
            )
        } else {
            localStore.persistDetail(pageLoad.owner, completeDetail, pageLoad.serverGeneration)
            pageSessions.remove(pageLoad.sessionKey)
        }
    }

    private companion object {
        const val MAX_ACTIVE_PAGE_SESSIONS = 4
        const val PLAYLIST_PAGE_LIMIT = 100
        const val SQLITE_SAFE_BATCH_SIZE = 900
    }

    private fun storePageSession(key: String, session: PlaylistPageSession) {
        synchronized(pageSessionsLock) {
            pageSessions.remove(key)
            while (pageSessions.size >= MAX_ACTIVE_PAGE_SESSIONS) {
                pageSessions.keys.firstOrNull()?.let(pageSessions::remove) ?: break
            }
            pageSessions[key] = session
        }
    }

    private data class PlaylistPageSession(
        val serverGeneration: ServerGeneration,
        val accumulator: PlaylistPageAccumulator,
    )

    private data class PlaylistPageLoad(
        val owner: String,
        val serverGeneration: ServerGeneration,
        val sessionKey: String,
        val activeSession: PlaylistPageSession?,
    )
}
