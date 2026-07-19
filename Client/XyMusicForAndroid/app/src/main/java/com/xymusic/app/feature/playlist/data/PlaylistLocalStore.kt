package com.xymusic.app.feature.playlist.data

import androidx.room.withTransaction
import com.xymusic.app.core.data.media.CatalogLocalDataSource
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.dao.CatalogDao
import com.xymusic.app.core.database.dao.PlaylistDao
import com.xymusic.app.core.database.dao.PlaylistSnapshot
import com.xymusic.app.core.database.entity.PlaylistEntity
import com.xymusic.app.core.database.entity.PlaylistEntryEntity
import com.xymusic.app.core.network.ServerGeneration
import com.xymusic.app.feature.playlist.data.remote.PlaylistDetailDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistEntryMutationDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistMutationDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistProtocolException
import com.xymusic.app.feature.playlist.data.remote.PlaylistSummaryDto
import com.xymusic.app.feature.playlist.domain.model.RemovePlaylistTrackCommand
import com.xymusic.app.feature.playlist.domain.model.ReorderPlaylistCommand
import java.time.Clock
import java.time.Instant

internal class PlaylistLocalStore(
    private val database: XyMusicDatabase,
    private val playlistDao: PlaylistDao,
    private val catalogDao: CatalogDao,
    private val catalogLocal: CatalogLocalDataSource,
    private val clock: Clock,
    private val executionContext: PlaylistRepositoryExecutionContext,
) {
    suspend fun persistPagePreview(owner: String, page: PlaylistDetailDto, serverGeneration: ServerGeneration) {
        val playlist = page.toEntity(owner)
        val cachedAtEpochMs = clock.millis()
        executionContext.withActiveOwner(owner, serverGeneration) {
            catalogLocal.mergeTrackSummaries(
                page.entries.map { entry -> entry.track },
                cachedAtEpochMs,
            )
            database.withTransaction {
                executionContext.requireCurrent(serverGeneration)
                val current = playlistDao.playlist(owner, page.id)
                if (current != null && current.version > playlist.version) {
                    throw PlaylistProtocolException("Playlist page is older than the cached resource")
                }
                if (current != null && current.version != playlist.version) {
                    playlistDao.replaceEntries(owner, page.id, emptyList())
                }
                playlistDao.upsertPlaylist(playlist)
            }
        }
    }

    suspend fun persistDetail(owner: String, detail: PlaylistDetailDto, serverGeneration: ServerGeneration) {
        val playlist = detail.toEntity(owner)
        val entries = detail.entries.map { entry -> entry.toEntity(owner, detail.id) }
        require(playlist.trackCount == entries.size) {
            "Playlist detail track count is inconsistent"
        }
        val cachedAtEpochMs = clock.millis()
        detail.entries.chunked(SQLITE_SAFE_BATCH_SIZE).forEach { entryBatch ->
            executionContext.withActiveOwner(owner) {
                executionContext.requireCurrent(serverGeneration)
                catalogLocal.mergeTrackSummaries(
                    entryBatch.map { entry -> entry.track },
                    cachedAtEpochMs,
                )
            }
        }
        executionContext.withActiveOwner(owner) {
            database.withTransaction {
                executionContext.requireCurrent(serverGeneration)
                val current = playlistDao.playlist(owner, detail.id)
                when {
                    current != null && current.version > playlist.version -> Unit
                    current != null &&
                        current.version == playlist.version &&
                        playlistDao.entryCount(owner, detail.id) == entries.size -> {
                        playlistDao.upsertPlaylist(playlist)
                    }
                    else -> playlistDao.replacePlaylist(playlist, entries)
                }
            }
        }
    }

    suspend fun persistUpdatedSummary(
        owner: String,
        playlistId: String,
        expectedVersion: Long,
        summary: PlaylistSummaryDto,
        serverGeneration: ServerGeneration,
    ): PlaylistEntity {
        require(summary.id == playlistId) { "Playlist update returned another resource" }
        val updated = summary.toEntity(owner)
        executionContext.withActiveOwner(owner, serverGeneration) {
            database.withTransaction {
                val current = playlistDao.playlist(owner, playlistId)
                if (current == null) {
                    playlistDao.upsertPlaylist(updated)
                } else if (current.version <= updated.version) {
                    val entriesRemainValid =
                        current.version == expectedVersion &&
                            current.trackCount == updated.trackCount
                    if (!entriesRemainValid) {
                        playlistDao.replaceEntries(owner, playlistId, emptyList())
                    }
                    playlistDao.upsertPlaylist(updated)
                }
            }
        }
        return updated
    }

    suspend fun deletePlaylist(owner: String, playlistId: String, serverGeneration: ServerGeneration) {
        executionContext.withActiveOwner(owner, serverGeneration) {
            playlistDao.deletePlaylist(owner, playlistId)
        }
    }

    suspend fun applyServerAddMutation(
        owner: String,
        playlistId: String,
        trackId: String,
        expectedVersion: Long,
        mutation: PlaylistEntryMutationDto,
        serverGeneration: ServerGeneration,
    ) {
        require(mutation.playlistId == playlistId) { "Playlist add returned another resource" }
        require(mutation.entry.track.id == trackId) { "Playlist add returned another track" }
        require(mutation.version == expectedVersion + 1) { "Playlist add returned an unexpected version" }
        executionContext.withActiveOwner(owner, serverGeneration) {
            catalogLocal.mergeTrackSummaries(listOf(mutation.entry.track), clock.millis())
            database.withTransaction {
                val snapshot = playlistDao.snapshot(owner, playlistId) ?: return@withTransaction
                when {
                    snapshot.playlist.version > mutation.version -> Unit
                    snapshot.playlist.version == mutation.version -> {
                        val alreadyApplied =
                            snapshot.hasCompleteEntries() &&
                                snapshot.entries.any { entry ->
                                    entry.id == mutation.entry.id && entry.trackId == trackId
                                }
                        if (!alreadyApplied) {
                            playlistDao.replaceEntries(owner, playlistId, emptyList())
                        }
                    }
                    snapshot.playlist.version != expectedVersion -> {
                        playlistDao.deletePlaylist(owner, playlistId)
                    }
                    else -> applyAddToExpectedSnapshot(owner, snapshot, mutation)
                }
            }
        }
    }

    suspend fun applyServerRemoveMutation(
        owner: String,
        command: RemovePlaylistTrackCommand,
        mutation: PlaylistMutationDto,
        serverGeneration: ServerGeneration,
    ) {
        require(mutation.playlistId == command.playlistId) { "Playlist remove returned another resource" }
        require(mutation.version == command.expectedVersion + 1) { "Playlist remove returned an unexpected version" }
        executionContext.withActiveOwner(owner, serverGeneration) {
            database.withTransaction {
                val snapshot = playlistDao.snapshot(owner, command.playlistId) ?: return@withTransaction
                when {
                    snapshot.playlist.version > mutation.version -> Unit
                    snapshot.playlist.version == mutation.version -> {
                        val alreadyApplied =
                            snapshot.hasCompleteEntries() &&
                                snapshot.entries.none { entry -> entry.id == command.entryId }
                        if (!alreadyApplied) {
                            playlistDao.replaceEntries(owner, command.playlistId, emptyList())
                        }
                    }
                    snapshot.playlist.version != command.expectedVersion -> {
                        playlistDao.deletePlaylist(owner, command.playlistId)
                    }
                    else -> applyRemoveToExpectedSnapshot(snapshot, command, mutation)
                }
            }
        }
    }

    suspend fun applyServerReorderMutation(
        owner: String,
        command: ReorderPlaylistCommand,
        mutation: PlaylistMutationDto,
        serverGeneration: ServerGeneration,
    ) {
        require(mutation.playlistId == command.playlistId) { "Playlist reorder returned another resource" }
        require(mutation.version == command.expectedVersion + 1) { "Playlist reorder returned an unexpected version" }
        executionContext.withActiveOwner(owner, serverGeneration) {
            database.withTransaction {
                val snapshot = playlistDao.snapshot(owner, command.playlistId) ?: return@withTransaction
                when {
                    snapshot.playlist.version > mutation.version -> Unit
                    snapshot.playlist.version == mutation.version -> {
                        val alreadyApplied =
                            snapshot.hasCompleteEntries() &&
                                snapshot.entries.sortedBy(PlaylistEntryEntity::position).map(PlaylistEntryEntity::id) ==
                                command.orderedEntryIds
                        if (!alreadyApplied) {
                            playlistDao.replaceEntries(owner, command.playlistId, emptyList())
                        }
                    }
                    snapshot.playlist.version != command.expectedVersion -> {
                        playlistDao.deletePlaylist(owner, command.playlistId)
                    }
                    else -> applyReorderToExpectedSnapshot(snapshot, command, mutation)
                }
            }
        }
    }

    private suspend fun applyAddToExpectedSnapshot(
        owner: String,
        snapshot: PlaylistSnapshot,
        mutation: PlaylistEntryMutationDto,
    ) {
        val replacement = mutation.entry.toEntity(owner, mutation.playlistId)
        val canApply =
            snapshot.hasCompleteEntries() &&
                snapshot.entries.none { entry ->
                    entry.id == replacement.id || entry.trackId == replacement.trackId
                } &&
                replacement.position in 0..snapshot.entries.size
        val updatedAt = Instant.parse(mutation.updatedAt).toEpochMilli()
        if (!canApply) {
            val addedArtwork = catalogDao.track(replacement.trackId)?.artwork
            playlistDao.replacePlaylist(
                snapshot.playlist.copy(
                    cover = addedArtwork,
                    trackCount = snapshot.playlist.trackCount + 1,
                    version = mutation.version,
                    updatedAtEpochMs = updatedAt,
                ),
                emptyList(),
            )
            return
        }

        val entries =
            snapshot.entries
                .toMutableList()
                .apply { add(replacement.position, replacement) }
                .mapIndexed { position, entry -> entry.copy(position = position) }
        playlistDao.replacePlaylist(
            snapshot.playlist.copy(
                cover = latestCover(entries),
                trackCount = entries.size,
                version = mutation.version,
                updatedAtEpochMs = updatedAt,
            ),
            entries,
        )
    }

    private suspend fun applyRemoveToExpectedSnapshot(
        snapshot: PlaylistSnapshot,
        command: RemovePlaylistTrackCommand,
        mutation: PlaylistMutationDto,
    ) {
        val canApply = snapshot.hasCompleteEntries() && snapshot.entries.any { entry -> entry.id == command.entryId }
        val updatedAt = Instant.parse(mutation.updatedAt).toEpochMilli()
        if (!canApply) {
            playlistDao.replacePlaylist(
                snapshot.playlist.copy(
                    cover = null,
                    trackCount = (snapshot.playlist.trackCount - 1).coerceAtLeast(0),
                    version = mutation.version,
                    updatedAtEpochMs = updatedAt,
                ),
                emptyList(),
            )
            return
        }

        val entries =
            snapshot.entries
                .filterNot { entry -> entry.id == command.entryId }
                .mapIndexed { position, entry -> entry.copy(position = position) }
        playlistDao.replacePlaylist(
            snapshot.playlist.copy(
                cover = latestCover(entries),
                trackCount = entries.size,
                version = mutation.version,
                updatedAtEpochMs = updatedAt,
            ),
            entries,
        )
    }

    private suspend fun applyReorderToExpectedSnapshot(
        snapshot: PlaylistSnapshot,
        command: ReorderPlaylistCommand,
        mutation: PlaylistMutationDto,
    ) {
        val byId = snapshot.entries.associateBy(PlaylistEntryEntity::id)
        val canApply =
            snapshot.hasCompleteEntries() &&
                byId.keys == command.orderedEntryIds.toSet()
        val updatedAt = Instant.parse(mutation.updatedAt).toEpochMilli()
        if (!canApply) {
            playlistDao.replacePlaylist(
                snapshot.playlist.copy(
                    version = mutation.version,
                    updatedAtEpochMs = updatedAt,
                ),
                emptyList(),
            )
            return
        }

        val entries =
            command.orderedEntryIds.mapIndexed { position, entryId ->
                requireNotNull(byId[entryId]).copy(position = position)
            }
        playlistDao.replacePlaylist(
            snapshot.playlist.copy(
                version = mutation.version,
                updatedAtEpochMs = updatedAt,
            ),
            entries,
        )
    }

    private suspend fun latestCover(entries: List<PlaylistEntryEntity>) =
        latestCoverTrackId(entries)?.let { trackId -> catalogDao.track(trackId)?.artwork }

    private companion object {
        const val SQLITE_SAFE_BATCH_SIZE = 900
    }
}

internal fun latestCoverTrackId(entries: List<PlaylistEntryEntity>): String? = entries
    .maxWithOrNull(
        compareBy<PlaylistEntryEntity>(PlaylistEntryEntity::addedAtEpochMs)
            .thenBy(PlaylistEntryEntity::id),
    )?.trackId

internal fun PlaylistSnapshot.hasCompleteEntries(): Boolean = playlist.trackCount == entries.size
