package com.xymusic.app.feature.playlist.data

import com.xymusic.app.core.database.dao.PlaylistDao
import com.xymusic.app.feature.playlist.data.remote.AddPlaylistTrackRequestDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistEntryMutationDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistRemoteDataSource
import com.xymusic.app.feature.playlist.data.remote.PlaylistRemoteException
import com.xymusic.app.feature.playlist.data.remote.PlaylistUpdatePayload
import com.xymusic.app.feature.playlist.data.remote.ReorderPlaylistRequestDto
import com.xymusic.app.feature.playlist.domain.PlaylistResult
import com.xymusic.app.feature.playlist.domain.model.AddPlaylistTrackCommand
import com.xymusic.app.feature.playlist.domain.model.PlaylistSummary
import com.xymusic.app.feature.playlist.domain.model.PlaylistVersionConflict
import com.xymusic.app.feature.playlist.domain.model.RemovePlaylistTrackCommand
import com.xymusic.app.feature.playlist.domain.model.ReorderPlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.UpdatePlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.ValueChange
import java.util.UUID

internal class PlaylistMutationOperations(
    private val playlistDao: PlaylistDao,
    private val remote: PlaylistRemoteDataSource,
    private val executionContext: PlaylistRepositoryExecutionContext,
    private val localStore: PlaylistLocalStore,
) {
    suspend fun update(command: UpdatePlaylistCommand): PlaylistResult<PlaylistSummary> =
        executionContext.serializePlaylistMutation(command.playlistId) {
            val serverGeneration = executionContext.captureServerGeneration()
            val owner = executionContext.requireOwner()
            localVersionConflict(owner, command.playlistId, command.expectedVersion)?.let {
                return@serializePlaylistMutation it
            }
            val result =
                remote.update(
                    command.playlistId,
                    UUID.randomUUID().toString(),
                    command.toPayload(),
                )
            val entity =
                localStore.persistUpdatedSummary(
                    owner = owner,
                    playlistId = command.playlistId,
                    expectedVersion = command.expectedVersion,
                    summary = result,
                    serverGeneration = serverGeneration,
                )
            PlaylistResult.Success(entity.toDomain())
        }

    suspend fun delete(playlistId: String, expectedVersion: Long): PlaylistResult<Unit> =
        executionContext.serializePlaylistMutation(playlistId) {
            val serverGeneration = executionContext.captureServerGeneration()
            val owner = executionContext.requireOwner()
            localVersionConflict(owner, playlistId, expectedVersion)?.let {
                return@serializePlaylistMutation it
            }
            remote.delete(
                playlistId,
                expectedVersion,
                UUID.randomUUID().toString(),
            )
            localStore.deletePlaylist(owner, playlistId, serverGeneration)
            PlaylistResult.Success(Unit)
        }

    suspend fun addTrack(command: AddPlaylistTrackCommand): PlaylistResult<Unit> =
        executionContext.serializePlaylistMutation(command.playlistId) {
            val serverGeneration = executionContext.captureServerGeneration()
            val owner = executionContext.requireOwner()
            val (expectedVersion, mutation) =
                addTrackWithOneVersionRefresh(
                    owner = owner,
                    command = command,
                    serverGeneration = serverGeneration,
                )
            localStore.applyServerAddMutation(
                owner = owner,
                playlistId = command.playlistId,
                trackId = command.trackId,
                expectedVersion = expectedVersion,
                mutation = mutation,
                serverGeneration = serverGeneration,
            )
            PlaylistResult.Success(Unit)
        }

    suspend fun removeTrack(command: RemovePlaylistTrackCommand): PlaylistResult<Unit> =
        executionContext.serializePlaylistMutation(command.playlistId) {
            val serverGeneration = executionContext.captureServerGeneration()
            val owner = executionContext.requireOwner()
            localVersionConflict(owner, command.playlistId, command.expectedVersion)?.let {
                return@serializePlaylistMutation it
            }
            val mutation =
                remote.removeTrack(
                    command.playlistId,
                    command.entryId,
                    command.expectedVersion,
                    UUID.randomUUID().toString(),
                )
            localStore.applyServerRemoveMutation(
                owner = owner,
                command = command,
                mutation = mutation,
                serverGeneration = serverGeneration,
            )
            PlaylistResult.Success(Unit)
        }

    suspend fun reorder(command: ReorderPlaylistCommand): PlaylistResult<Unit> =
        executionContext.serializePlaylistMutation(command.playlistId) {
            val serverGeneration = executionContext.captureServerGeneration()
            val owner = executionContext.requireOwner()
            localVersionConflict(owner, command.playlistId, command.expectedVersion)?.let {
                return@serializePlaylistMutation it
            }
            val mutation =
                remote.reorder(
                    command.playlistId,
                    UUID.randomUUID().toString(),
                    ReorderPlaylistRequestDto(
                        command.expectedVersion,
                        command.orderedEntryIds,
                    ),
                )
            localStore.applyServerReorderMutation(
                owner = owner,
                command = command,
                mutation = mutation,
                serverGeneration = serverGeneration,
            )
            PlaylistResult.Success(Unit)
        }

    private suspend fun addTrackWithOneVersionRefresh(
        owner: String,
        command: AddPlaylistTrackCommand,
        serverGeneration: com.xymusic.app.core.network.ServerGeneration,
    ): Pair<Long, PlaylistEntryMutationDto> {
        val firstExpectedVersion = command.expectedVersion
        try {
            return firstExpectedVersion to requestAdd(command, firstExpectedVersion)
        } catch (failure: PlaylistRemoteException) {
            if (failure.conflict == null) throw failure

            val latest = remote.playlistPage(command.playlistId, cursor = null, limit = METADATA_PAGE_LIMIT)
            require(latest.id == command.playlistId) {
                "Playlist metadata refresh returned another resource"
            }
            require(latest.owner.id == owner) {
                "Playlist metadata refresh returned another owner"
            }
            localStore.persistPagePreview(owner, latest, serverGeneration)
            if (latest.version == firstExpectedVersion) throw failure
            return latest.version to requestAdd(command, latest.version)
        }
    }

    private suspend fun requestAdd(
        command: AddPlaylistTrackCommand,
        expectedVersion: Long,
    ): PlaylistEntryMutationDto =
        remote.addTrack(
            command.playlistId,
            UUID.randomUUID().toString(),
            AddPlaylistTrackRequestDto(
                expectedVersion = expectedVersion,
                trackId = command.trackId,
                insertAfterEntryId = command.insertAfterEntryId,
            ),
        )

    private suspend fun localVersionConflict(
        owner: String,
        playlistId: String,
        expectedVersion: Long,
    ): PlaylistResult.Conflict? {
        val currentVersion = playlistDao.playlist(owner, playlistId)?.version ?: return null
        return if (currentVersion == expectedVersion) {
            null
        } else {
            PlaylistResult.Conflict(
                PlaylistVersionConflict(
                    playlistId = playlistId,
                    expectedVersion = expectedVersion,
                    currentVersion = currentVersion,
                    conflictFields = emptySet(),
                ),
            )
        }
    }

    private fun UpdatePlaylistCommand.toPayload() = PlaylistUpdatePayload(
        expectedVersion = expectedVersion,
        namePresent = name is ValueChange.Set,
        name =
        when (val change = name) {
            ValueChange.Unchanged -> null
            is ValueChange.Set -> change.value
        },
        descriptionPresent = description is ValueChange.Set,
        description =
        when (val change = description) {
            ValueChange.Unchanged -> null
            is ValueChange.Set -> change.value
        },
        visibilityPresent = visibility is ValueChange.Set,
        visibility =
        when (val change = visibility) {
            ValueChange.Unchanged -> null
            is ValueChange.Set -> change.value.name
        },
    )

    private companion object {
        const val METADATA_PAGE_LIMIT = 1
    }
}
