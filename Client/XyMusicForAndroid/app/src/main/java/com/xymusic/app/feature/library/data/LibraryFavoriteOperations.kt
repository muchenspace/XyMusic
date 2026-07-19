package com.xymusic.app.feature.library.data

import androidx.room.withTransaction
import com.xymusic.app.core.data.media.CatalogLocalDataSource
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.dao.CatalogDao
import com.xymusic.app.core.database.dao.LibraryDao
import com.xymusic.app.core.database.dao.PendingSyncOperationDao
import com.xymusic.app.core.database.entity.FavoriteEntity
import com.xymusic.app.core.database.entity.PendingSyncOperationEntity
import com.xymusic.app.core.database.model.SyncOperationStatus
import com.xymusic.app.core.database.model.SyncOperationType
import com.xymusic.app.core.database.model.SyncTargetType
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.network.ServerGeneration
import com.xymusic.app.core.sync.PendingSyncScheduler
import com.xymusic.app.feature.library.data.remote.FavoriteItemDto
import com.xymusic.app.feature.library.data.remote.LibraryProtocolException
import com.xymusic.app.feature.library.data.remote.LibraryRemoteDataSource
import com.xymusic.app.feature.library.data.remote.LibraryRemoteException
import com.xymusic.app.feature.library.data.sync.FavoritePendingPayload
import com.xymusic.app.feature.library.domain.LibraryResult
import com.xymusic.app.feature.library.domain.model.FavoriteSort
import java.io.IOException
import java.time.Clock
import java.time.Instant
import java.util.UUID
import java.util.concurrent.CancellationException
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

internal class LibraryFavoriteOperations(
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
    private val mutationCoordinator: FavoriteMutationCoordinator,
) {
    private val refreshMutex = Mutex()

    suspend fun refreshFavorites(sort: FavoriteSort): LibraryResult<Unit> = refreshMutex.withLock {
        executionContext.ioCall {
            val serverGeneration = executionContext.captureServerGeneration()
            val owner = executionContext.requireOwner()
            val refreshStart =
                executionContext.withActiveOwner(owner, serverGeneration) {
                    FavoriteRefreshStart(
                        favoritesByTrackId =
                        libraryDao
                            .favorites(owner)
                            .associateBy(FavoriteEntity::trackId),
                        mutationSnapshot = mutationCoordinator.captureRefreshSnapshot(),
                    )
                }
            reconcileFavorites(
                owner = owner,
                items = remote.allFavorites(sort.name),
                serverGeneration = serverGeneration,
                refreshStart = refreshStart,
            )
            LibraryResult.Success(Unit)
        }
    }

    suspend fun setFavorite(trackId: String, favorite: Boolean): LibraryResult<Unit> =
        mutationCoordinator.serialize(trackId) {
            executionContext.ioCall {
                executeFavoriteMutation(prepareFavoriteMutation(trackId, favorite))
            }
        }

    private suspend fun prepareFavoriteMutation(trackId: String, favorite: Boolean): FavoriteMutation {
        val serverGeneration = executionContext.captureServerGeneration()
        val owner = executionContext.requireOwner()
        val mutationStart = executionContext.withActiveOwner(owner, serverGeneration) {
            if (favorite && catalogDao.track(trackId) == null) {
                throw LocalLibraryException("Track is not cached")
            }
            val olderOperations = pendingDao
                .forTarget(owner, SyncTargetType.FAVORITE, trackId)
                .filter { operation ->
                    operation.status != SyncOperationStatus.CONFLICT &&
                        operation.operationType in FAVORITE_OPERATION_TYPES
                }
            FavoriteMutationStart(
                previous = libraryDao.favorite(owner, trackId),
                latestPendingCreatedAtEpochMs = olderOperations
                    .asSequence()
                    .maxOfOrNull(PendingSyncOperationEntity::createdAtEpochMs),
                latestPendingNextAttemptAtEpochMs = olderOperations
                    .asSequence()
                    .maxOfOrNull(PendingSyncOperationEntity::nextAttemptAtEpochMs),
            )
        }
        val now = clock.millis()
        return FavoriteMutation(
            owner = owner,
            trackId = trackId,
            favorite = favorite,
            start = mutationStart,
            serverGeneration = serverGeneration,
            timing = FavoriteMutationTiming(
                stateChangedAtEpochMs = now,
                operationCreatedAtEpochMs = nextFavoriteOperationCreatedAt(
                    nowEpochMs = now,
                    latestPendingCreatedAtEpochMs = mutationStart.latestPendingCreatedAtEpochMs,
                ),
                operationNextAttemptAtEpochMs = maxOf(
                    now,
                    mutationStart.latestPendingNextAttemptAtEpochMs ?: now,
                ),
            ),
        )
    }

    private suspend fun executeFavoriteMutation(mutation: FavoriteMutation): LibraryResult<Unit> = try {
        executionContext.withActiveOwner(mutation.owner, mutation.serverGeneration) {
            applyFavoriteState(
                mutation.owner,
                mutation.trackId,
                mutation.favorite,
                mutation.timing.stateChangedAtEpochMs,
            )
        }
        val outcome = synchronizeFavorite(mutation)
        preserveOlderPendingOrder(mutation, outcome)
        outcome.result
    } catch (failure: CancellationException) {
        recoverCancelledFavorite(mutation, failure)
    }

    private suspend fun synchronizeFavorite(mutation: FavoriteMutation): FavoriteRemoteOutcome = try {
        applyRemoteFavoriteMutation(mutation)
        FavoriteRemoteOutcome(
            result = LibraryResult.Success(Unit),
            remoteSucceeded = true,
            pendingPersisted = false,
        )
    } catch (failure: LibraryRemoteException) {
        handleFavoriteRemoteFailure(mutation, failure)
    } catch (failure: LibraryProtocolException) {
        withContext(NonCancellable) {
            restorePreviousFavorite(mutation)
        }
        throw failure
    } catch (_: IOException) {
        persistPendingFavorite(mutation)
        FavoriteRemoteOutcome(
            result = LibraryResult.Success(Unit),
            remoteSucceeded = false,
            pendingPersisted = true,
        )
    }

    private suspend fun applyRemoteFavoriteMutation(mutation: FavoriteMutation) {
        if (mutation.favorite) {
            val item = remote.addFavorite(mutation.trackId)
            persistFavoriteItems(
                owner = mutation.owner,
                items = listOf(item),
                serverGeneration = mutation.serverGeneration,
            )
        } else {
            remote.removeFavorite(mutation.trackId)
            executionContext.withActiveOwner(mutation.owner, mutation.serverGeneration) { Unit }
        }
    }

    private suspend fun handleFavoriteRemoteFailure(
        mutation: FavoriteMutation,
        failure: LibraryRemoteException,
    ): FavoriteRemoteOutcome = if (failure.error.isRetryable()) {
        persistPendingFavorite(mutation)
        FavoriteRemoteOutcome(
            result = LibraryResult.Success(Unit),
            remoteSucceeded = false,
            pendingPersisted = true,
        )
    } else {
        withContext(NonCancellable) {
            restorePreviousFavorite(mutation)
        }
        FavoriteRemoteOutcome(
            result = LibraryResult.Failure(failure.error),
            remoteSucceeded = false,
            pendingPersisted = false,
        )
    }

    private suspend fun persistPendingFavorite(mutation: FavoriteMutation) {
        withContext(NonCancellable) {
            enqueueFavoriteOrRollback(
                owner = mutation.owner,
                trackId = mutation.trackId,
                favorite = mutation.favorite,
                stateChangedAtEpochMs = mutation.timing.stateChangedAtEpochMs,
                operationCreatedAtEpochMs = mutation.timing.operationCreatedAtEpochMs,
                operationNextAttemptAtEpochMs = mutation.timing.operationNextAttemptAtEpochMs,
                previous = mutation.start.previous,
                serverGeneration = mutation.serverGeneration,
            )
        }
    }

    private suspend fun preserveOlderPendingOrder(mutation: FavoriteMutation, outcome: FavoriteRemoteOutcome) {
        if (
            outcome.remoteSucceeded &&
            !outcome.pendingPersisted &&
            mutation.start.latestPendingCreatedAtEpochMs != null
        ) {
            withContext(NonCancellable) {
                enqueueFavorite(
                    owner = mutation.owner,
                    trackId = mutation.trackId,
                    favorite = mutation.favorite,
                    stateChangedAtEpochMs = mutation.timing.stateChangedAtEpochMs,
                    operationCreatedAtEpochMs = mutation.timing.operationCreatedAtEpochMs,
                    operationNextAttemptAtEpochMs = mutation.timing.operationNextAttemptAtEpochMs,
                    serverGeneration = mutation.serverGeneration,
                    applyLocalState = false,
                )
            }
        }
    }

    private suspend fun restorePreviousFavorite(mutation: FavoriteMutation) {
        rollbackFavorite(
            mutation.owner,
            mutation.trackId,
            mutation.start.previous,
            mutation.serverGeneration,
        )
    }

    private suspend fun recoverCancelledFavorite(mutation: FavoriteMutation, failure: CancellationException): Nothing {
        val recoveryFailure = withContext(NonCancellable) {
            runCatching {
                enqueueFavoriteOrRollback(
                    owner = mutation.owner,
                    trackId = mutation.trackId,
                    favorite = mutation.favorite,
                    stateChangedAtEpochMs = mutation.timing.stateChangedAtEpochMs,
                    operationCreatedAtEpochMs = mutation.timing.operationCreatedAtEpochMs,
                    operationNextAttemptAtEpochMs = mutation.timing.operationNextAttemptAtEpochMs,
                    previous = mutation.start.previous,
                    serverGeneration = mutation.serverGeneration,
                )
            }.exceptionOrNull()
        }
        recoveryFailure?.let(failure::addSuppressed)
        throw failure
    }

    private suspend fun persistFavoriteItems(
        owner: String,
        items: List<FavoriteItemDto>,
        serverGeneration: ServerGeneration,
    ) {
        val cachedAt = clock.millis()
        executionContext.withActiveOwner(owner, serverGeneration) {
            database.withTransaction {
                catalogLocal.mergeTrackSummaries(items.map(FavoriteItemDto::track), cachedAt)
                items.forEach { item ->
                    libraryDao.upsertFavorite(
                        FavoriteEntity(
                            owner,
                            item.track.id,
                            Instant.parse(item.favoritedAt).toEpochMilli(),
                        ),
                    )
                }
            }
        }
    }

    private suspend fun reconcileFavorites(
        owner: String,
        items: List<FavoriteItemDto>,
        serverGeneration: ServerGeneration,
        refreshStart: FavoriteRefreshStart,
    ) {
        val cachedAt = clock.millis()
        executionContext.withActiveOwner(owner, serverGeneration) {
            database.withTransaction {
                catalogLocal.mergeTrackSummaries(items.map(FavoriteItemDto::track), cachedAt)
                val currentFavoritesByTrackId =
                    libraryDao
                        .favorites(owner)
                        .associateBy(FavoriteEntity::trackId)
                val protectedTrackIds =
                    mutationCoordinator
                        .protectedTrackIdsSince(refreshStart.mutationSnapshot)
                        .toMutableSet()
                (refreshStart.favoritesByTrackId.keys + currentFavoritesByTrackId.keys)
                    .filterTo(protectedTrackIds) { trackId ->
                        refreshStart.favoritesByTrackId[trackId] !=
                            currentFavoritesByTrackId[trackId]
                    }
                libraryDao.deleteFavorites(owner)
                items.forEach { item ->
                    libraryDao.upsertFavorite(
                        FavoriteEntity(
                            owner,
                            item.track.id,
                            Instant.parse(item.favoritedAt).toEpochMilli(),
                        ),
                    )
                }
                protectedTrackIds.forEach { trackId ->
                    val current = currentFavoritesByTrackId[trackId]
                    if (current == null) {
                        libraryDao.deleteFavorite(owner, trackId)
                    } else {
                        libraryDao.upsertFavorite(current)
                    }
                }
                val operations =
                    pendingDao.actionableForTargetType(
                        owner,
                        SyncTargetType.FAVORITE,
                    )
                val pendingAddTrackIds =
                    operations
                        .asSequence()
                        .filter { it.operationType == SyncOperationType.ADD_FAVORITE }
                        .mapNotNull(PendingSyncOperationEntity::targetId)
                        .distinct()
                        .toList()
                val cachedTrackIds: Set<String> =
                    if (pendingAddTrackIds.isEmpty()) {
                        emptySet()
                    } else {
                        catalogDao.tracks(pendingAddTrackIds).mapTo(hashSetOf()) { it.track.id }
                    }
                operations.forEach { operation ->
                    val trackId = operation.targetId ?: return@forEach
                    when (operation.operationType) {
                        SyncOperationType.ADD_FAVORITE -> {
                            if (trackId in cachedTrackIds) {
                                libraryDao.upsertFavorite(
                                    FavoriteEntity(owner, trackId, operation.createdAtEpochMs),
                                )
                            }
                        }
                        SyncOperationType.REMOVE_FAVORITE -> {
                            libraryDao.deleteFavorite(owner, trackId)
                        }
                        else -> Unit
                    }
                }
            }
        }
    }

    private suspend fun enqueueFavorite(
        owner: String,
        trackId: String,
        favorite: Boolean,
        stateChangedAtEpochMs: Long,
        operationCreatedAtEpochMs: Long,
        operationNextAttemptAtEpochMs: Long,
        serverGeneration: ServerGeneration,
        applyLocalState: Boolean = true,
    ) {
        val operation =
            PendingSyncOperationEntity(
                ownerUserId = owner,
                id = UUID.randomUUID().toString(),
                operationType =
                if (favorite) {
                    SyncOperationType.ADD_FAVORITE
                } else {
                    SyncOperationType.REMOVE_FAVORITE
                },
                targetType = SyncTargetType.FAVORITE,
                targetId = trackId,
                requestPayloadJson = json.encodeToString(FavoritePendingPayload(trackId)),
                idempotencyKey = UUID.randomUUID().toString(),
                status = SyncOperationStatus.PENDING,
                attemptCount = 0,
                createdAtEpochMs = operationCreatedAtEpochMs,
                updatedAtEpochMs = operationCreatedAtEpochMs,
                nextAttemptAtEpochMs = operationNextAttemptAtEpochMs,
                leaseOwner = null,
                leaseExpiresAtEpochMs = null,
                lastErrorCode = null,
            )
        executionContext.withActiveOwner(owner, serverGeneration) {
            database.withTransaction {
                if (applyLocalState) {
                    applyFavoriteState(owner, trackId, favorite, stateChangedAtEpochMs)
                }
                check(pendingDao.enqueue(operation) != -1L) {
                    "Unable to enqueue favorite operation"
                }
            }
            runCatching { pendingSyncScheduler.schedule(owner) }
        }
    }

    private suspend fun enqueueFavoriteOrRollback(
        owner: String,
        trackId: String,
        favorite: Boolean,
        stateChangedAtEpochMs: Long,
        operationCreatedAtEpochMs: Long,
        operationNextAttemptAtEpochMs: Long,
        previous: FavoriteEntity?,
        serverGeneration: ServerGeneration,
    ) {
        try {
            enqueueFavorite(
                owner = owner,
                trackId = trackId,
                favorite = favorite,
                stateChangedAtEpochMs = stateChangedAtEpochMs,
                operationCreatedAtEpochMs = operationCreatedAtEpochMs,
                operationNextAttemptAtEpochMs = operationNextAttemptAtEpochMs,
                serverGeneration = serverGeneration,
            )
        } catch (enqueueFailure: Exception) {
            try {
                rollbackFavorite(owner, trackId, previous, serverGeneration)
            } catch (rollbackFailure: Exception) {
                enqueueFailure.addSuppressed(rollbackFailure)
            }
            throw enqueueFailure
        }
    }

    private suspend fun applyFavoriteState(owner: String, trackId: String, favorite: Boolean, now: Long) {
        if (favorite) {
            libraryDao.upsertFavorite(FavoriteEntity(owner, trackId, now))
        } else {
            libraryDao.deleteFavorite(owner, trackId)
        }
    }

    private suspend fun rollbackFavorite(
        owner: String,
        trackId: String,
        previous: FavoriteEntity?,
        serverGeneration: ServerGeneration,
    ) {
        executionContext.withActiveOwner(owner, serverGeneration) {
            if (previous == null) {
                libraryDao.deleteFavorite(owner, trackId)
            } else {
                libraryDao.upsertFavorite(previous)
            }
        }
    }

    private fun nextFavoriteOperationCreatedAt(nowEpochMs: Long, latestPendingCreatedAtEpochMs: Long?): Long =
        latestPendingCreatedAtEpochMs?.let { latest ->
            maxOf(nowEpochMs, Math.addExact(latest, 1L))
        } ?: nowEpochMs

    private fun DomainError.isRetryable(): Boolean = this is DomainError.RateLimited ||
        this is DomainError.ServiceUnavailable ||
        this is DomainError.Server

    private data class FavoriteRefreshStart(
        val favoritesByTrackId: Map<String, FavoriteEntity>,
        val mutationSnapshot: FavoriteMutationSnapshot,
    )

    private data class FavoriteMutationStart(
        val previous: FavoriteEntity?,
        val latestPendingCreatedAtEpochMs: Long?,
        val latestPendingNextAttemptAtEpochMs: Long?,
    )

    private data class FavoriteMutation(
        val owner: String,
        val trackId: String,
        val favorite: Boolean,
        val start: FavoriteMutationStart,
        val serverGeneration: ServerGeneration,
        val timing: FavoriteMutationTiming,
    )

    private data class FavoriteMutationTiming(
        val stateChangedAtEpochMs: Long,
        val operationCreatedAtEpochMs: Long,
        val operationNextAttemptAtEpochMs: Long,
    )

    private data class FavoriteRemoteOutcome(
        val result: LibraryResult<Unit>,
        val remoteSucceeded: Boolean,
        val pendingPersisted: Boolean,
    )

    private companion object {
        val FAVORITE_OPERATION_TYPES =
            setOf(
                SyncOperationType.ADD_FAVORITE,
                SyncOperationType.REMOVE_FAVORITE,
            )
    }
}
