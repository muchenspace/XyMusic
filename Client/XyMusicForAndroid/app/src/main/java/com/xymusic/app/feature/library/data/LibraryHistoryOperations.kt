package com.xymusic.app.feature.library.data

import androidx.room.withTransaction
import com.xymusic.app.core.data.media.CatalogLocalDataSource
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.dao.CatalogDao
import com.xymusic.app.core.database.dao.LibraryDao
import com.xymusic.app.core.database.dao.PendingSyncOperationDao
import com.xymusic.app.core.database.entity.HistoryEntity
import com.xymusic.app.core.database.entity.PendingSyncOperationEntity
import com.xymusic.app.core.database.model.SyncOperationStatus
import com.xymusic.app.core.database.model.SyncOperationType
import com.xymusic.app.core.database.model.SyncTargetType
import com.xymusic.app.core.sync.PendingSyncScheduler
import com.xymusic.app.feature.library.data.remote.HistoryItemDto
import com.xymusic.app.feature.library.data.remote.LibraryRemoteDataSource
import com.xymusic.app.feature.library.data.sync.PlaybackPendingPayload
import com.xymusic.app.feature.library.domain.LibraryResult
import com.xymusic.app.feature.library.domain.model.PlaybackEvent
import com.xymusic.app.feature.library.domain.model.PlaybackProgressCommand
import java.time.Clock
import java.time.Instant
import java.util.UUID
import kotlinx.coroutines.flow.collect
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

internal class LibraryHistoryOperations(
    private val database: XyMusicDatabase,
    private val libraryDao: LibraryDao,
    private val catalogDao: CatalogDao,
    private val pendingDao: PendingSyncOperationDao,
    private val catalogLocal: CatalogLocalDataSource,
    private val remote: LibraryRemoteDataSource,
    private val executionContext: LibraryRepositoryExecutionContext,
    private val pendingSyncScheduler: PendingSyncScheduler,
    private val json: Json,
    private val clock: Clock,
) {
    suspend fun refreshHistory(): LibraryResult<Unit> = executionContext.ioCall {
        val owner = executionContext.requireOwner()
        remote.historyPages().collect { items -> persistHistoryPage(owner, items) }
        executionContext.withActiveOwner(owner) { Unit }
        LibraryResult.Success(Unit)
    }

    suspend fun recordPlayback(command: PlaybackProgressCommand): LibraryResult<Unit> = executionContext.ioCall {
        recordPlaybackForActiveOwner(executionContext.requireOwner(), command)
    }

    suspend fun recordPlaybackForOwner(ownerUserId: String, command: PlaybackProgressCommand): LibraryResult<Unit> =
        executionContext.ioCall {
            val owner = ownerUserId.takeIf(String::isNotBlank) ?: throw SignedOutException
            recordPlaybackForActiveOwner(owner, command)
        }

    private suspend fun recordPlaybackForActiveOwner(
        owner: String,
        command: PlaybackProgressCommand,
    ): LibraryResult<Unit> {
        executionContext.withActiveOwner(owner) {
            val previous = libraryDao.history(owner, command.trackId)
            if (catalogDao.track(command.trackId) == null) {
                throw LocalLibraryException("Track is not cached")
            }
            val optimistic = mergeLocalProgress(owner, previous, command)
            database.withTransaction {
                libraryDao.upsertHistory(optimistic)
                enqueuePlayback(owner, command)
            }
            runCatching { pendingSyncScheduler.schedule(owner) }
        }
        return LibraryResult.Success(Unit)
    }

    private suspend fun persistHistoryPage(owner: String, items: List<HistoryItemDto>) {
        if (items.isEmpty()) return
        val cachedAt = clock.millis()
        val remoteHistoryByTrack = linkedMapOf<String, HistoryEntity>()
        items.forEach { item ->
            val history = item.toHistoryEntity(owner)
            val existing = remoteHistoryByTrack[history.trackId]
            if (existing == null || history.updatedAtEpochMs >= existing.updatedAtEpochMs) {
                remoteHistoryByTrack[history.trackId] = history
            }
        }
        val trackIds = remoteHistoryByTrack.keys.toList()
        executionContext.withActiveOwner(owner) {
            val pendingPlaybackByTrack =
                pendingDao
                    .actionableForTargets(owner, SyncTargetType.PLAYBACK_HISTORY, trackIds)
                    .asSequence()
                    .filter { it.operationType == SyncOperationType.RECORD_PLAYBACK }
                    .mapNotNull { operation ->
                        val payload =
                            runCatching {
                                json.decodeFromString<PlaybackPendingPayload>(
                                    requireNotNull(operation.requestPayloadJson),
                                )
                            }.getOrNull() ?: return@mapNotNull null
                        payload.takeIf { it.trackId == operation.targetId }
                    }.groupBy(PlaybackPendingPayload::trackId)
            val mergedHistory =
                remoteHistoryByTrack.mapValues { (trackId, remoteHistory) ->
                    pendingPlaybackByTrack[trackId]
                        .orEmpty()
                        .fold(remoteHistory, ::applyPendingPlayback)
                }
            database.withTransaction {
                catalogLocal.mergeTrackSummaries(items.map(HistoryItemDto::track), cachedAt)
                val cachedHistoryByTrack =
                    libraryDao
                        .histories(owner, trackIds)
                        .associateBy(HistoryEntity::trackId)
                libraryDao.upsertHistories(
                    mergedHistory.map { (trackId, remoteHistory) ->
                        cachedHistoryByTrack[trackId]?.takeIf {
                            it.updatedAtEpochMs > remoteHistory.updatedAtEpochMs
                        } ?: remoteHistory
                    },
                )
            }
        }
    }

    private fun HistoryItemDto.toHistoryEntity(owner: String): HistoryEntity = HistoryEntity(
        ownerUserId = owner,
        trackId = track.id,
        lastPositionMs = lastPositionMs,
        playCount = playCount,
        lastPlayedAtEpochMs = Instant.parse(lastPlayedAt).toEpochMilli(),
        completed = completed,
        updatedAtEpochMs = Instant.parse(updatedAt).toEpochMilli(),
    )

    private fun applyPendingPlayback(history: HistoryEntity, payload: PlaybackPendingPayload): HistoryEntity {
        val occurredAtEpochMs =
            runCatching { Instant.parse(payload.occurredAt).toEpochMilli() }
                .getOrNull()
                ?: return history
        if (occurredAtEpochMs <= history.updatedAtEpochMs) return history
        val event =
            runCatching { PlaybackEvent.valueOf(payload.event) }.getOrNull()
                ?: return history
        return history.copy(
            lastPositionMs = payload.positionMs,
            playCount = history.playCount + if (event == PlaybackEvent.STARTED) 1 else 0,
            lastPlayedAtEpochMs = occurredAtEpochMs,
            completed = event == PlaybackEvent.COMPLETED,
            updatedAtEpochMs = occurredAtEpochMs,
        )
    }

    private fun mergeLocalProgress(
        owner: String,
        previous: HistoryEntity?,
        command: PlaybackProgressCommand,
    ): HistoryEntity = HistoryEntity(
        ownerUserId = owner,
        trackId = command.trackId,
        lastPositionMs = command.positionMs,
        playCount =
        (previous?.playCount ?: 0) +
            if (command.event == PlaybackEvent.STARTED) 1 else 0,
        lastPlayedAtEpochMs = command.occurredAtEpochMillis,
        completed = command.event == PlaybackEvent.COMPLETED,
        updatedAtEpochMs = command.occurredAtEpochMillis,
    )

    private suspend fun enqueuePlayback(owner: String, command: PlaybackProgressCommand) {
        val occurredAt = Instant.ofEpochMilli(command.occurredAtEpochMillis).toString()
        val now = clock.millis()
        val payload =
            json.encodeToString(
                PlaybackPendingPayload(
                    command.trackId,
                    command.playbackSessionId,
                    command.positionMs,
                    occurredAt,
                    command.event.name,
                ),
            )
        val progressPrefix = playbackProgressKeyPrefix(command.playbackSessionId)
        val operation =
            PendingSyncOperationEntity(
                ownerUserId = owner,
                id = UUID.randomUUID().toString(),
                operationType = SyncOperationType.RECORD_PLAYBACK,
                targetType = SyncTargetType.PLAYBACK_HISTORY,
                targetId = command.trackId,
                requestPayloadJson = payload,
                idempotencyKey =
                if (command.event == PlaybackEvent.PROGRESS) {
                    progressPrefix + UUID.randomUUID()
                } else {
                    UUID.randomUUID().toString()
                },
                status = SyncOperationStatus.PENDING,
                attemptCount = 0,
                createdAtEpochMs = now,
                updatedAtEpochMs = now,
                nextAttemptAtEpochMs = now,
                leaseOwner = null,
                leaseExpiresAtEpochMs = null,
                lastErrorCode = null,
            )
        if (command.event == PlaybackEvent.PROGRESS) {
            pendingDao.enqueueOrReplacePlaybackProgress(operation, progressPrefix)
        } else {
            check(pendingDao.enqueue(operation) != -1L) {
                "Unable to enqueue playback checkpoint"
            }
        }
    }

    private fun playbackProgressKeyPrefix(playbackSessionId: String): String = "playback-progress:$playbackSessionId:"
}
