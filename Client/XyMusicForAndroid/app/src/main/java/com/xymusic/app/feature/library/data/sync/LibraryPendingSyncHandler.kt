package com.xymusic.app.feature.library.data.sync

import com.xymusic.app.core.database.entity.PendingSyncOperationEntity
import com.xymusic.app.core.database.model.SyncOperationType
import com.xymusic.app.core.network.toWireValue
import com.xymusic.app.core.sync.PendingExecutionOutcome
import com.xymusic.app.core.sync.PendingSyncOperationHandler
import com.xymusic.app.core.sync.PendingSyncOwnerChangedException
import com.xymusic.app.core.sync.PendingSyncOwnerGuard
import com.xymusic.app.core.sync.PendingSyncPayloadCodec
import com.xymusic.app.domain.error.DomainError
import com.xymusic.app.feature.library.data.remote.LibraryProtocolException
import com.xymusic.app.feature.library.data.remote.LibraryRemoteDataSource
import com.xymusic.app.feature.library.data.remote.LibraryRemoteException
import com.xymusic.app.feature.library.data.remote.RecordPlaybackRequestDto
import java.io.IOException
import java.util.concurrent.CancellationException
import javax.inject.Inject
import kotlinx.serialization.SerializationException

class LibraryPendingSyncHandler
@Inject
constructor(
    private val libraryRemote: LibraryRemoteDataSource,
    private val ownerGuard: PendingSyncOwnerGuard,
    private val payloadCodec: PendingSyncPayloadCodec,
    private val libraryStore: LibraryPendingSyncStore,
) : PendingSyncOperationHandler {
    override val supportedOperationTypes: Set<SyncOperationType> = setOf(
        SyncOperationType.ADD_FAVORITE,
        SyncOperationType.REMOVE_FAVORITE,
        SyncOperationType.RECORD_PLAYBACK,
    )

    override suspend fun execute(operation: PendingSyncOperationEntity): PendingExecutionOutcome = try {
        when (operation.operationType) {
            SyncOperationType.ADD_FAVORITE -> addFavorite(operation)
            SyncOperationType.REMOVE_FAVORITE -> removeFavorite(operation)
            SyncOperationType.RECORD_PLAYBACK -> recordPlayback(operation)
            else -> PendingExecutionOutcome.Success
        }
    } catch (failure: CancellationException) {
        throw failure
    } catch (failure: Exception) {
        failure.toExecutionOutcome(operation)
    }

    private suspend fun addFavorite(operation: PendingSyncOperationEntity): PendingExecutionOutcome {
        val payload = payloadCodec.decode(operation, FavoritePendingPayload.serializer())
        requireTarget(operation, payload.trackId)
        val item = libraryRemote.addFavorite(payload.trackId)
        ownerGuard.mutateIfActive(operation.ownerUserId) {
            libraryStore.persistFavorite(operation, item)
        }
        return PendingExecutionOutcome.Success
    }

    private suspend fun removeFavorite(operation: PendingSyncOperationEntity): PendingExecutionOutcome {
        val payload = payloadCodec.decode(operation, FavoritePendingPayload.serializer())
        requireTarget(operation, payload.trackId)
        libraryRemote.removeFavorite(payload.trackId)
        ownerGuard.mutateIfActive(operation.ownerUserId) { Unit }
        return PendingExecutionOutcome.Success
    }

    private suspend fun recordPlayback(operation: PendingSyncOperationEntity): PendingExecutionOutcome {
        val payload = payloadCodec.decode(operation, PlaybackPendingPayload.serializer())
        requireTarget(operation, payload.trackId)
        val item =
            libraryRemote.recordPlayback(
                trackId = payload.trackId,
                idempotencyKey = operation.idempotencyKey,
                request =
                RecordPlaybackRequestDto(
                    playbackSessionId = payload.playbackSessionId,
                    positionMs = payload.positionMs,
                    occurredAt = payload.occurredAt,
                    event = payload.event,
                ),
            )
        ownerGuard.mutateIfActive(operation.ownerUserId) {
            libraryStore.persistHistory(operation.ownerUserId, item)
        }
        return PendingExecutionOutcome.Success
    }

    private fun requireTarget(operation: PendingSyncOperationEntity, payloadTarget: String) {
        require(operation.targetId == payloadTarget) { "Pending target does not match its payload" }
    }

    private fun Exception.toExecutionOutcome(operation: PendingSyncOperationEntity): PendingExecutionOutcome =
        when (this) {
            is PendingSyncOwnerChangedException -> PendingExecutionOutcome.OwnerChanged
            is LibraryRemoteException -> toLibraryExecutionOutcome(operation)
            is SerializationException,
            is IllegalArgumentException,
            -> PendingExecutionOutcome.Conflict(ERROR_INVALID_PAYLOAD)
            is LibraryProtocolException -> PendingExecutionOutcome.Conflict(ERROR_PROTOCOL)
            is IOException -> PendingExecutionOutcome.Retry(ERROR_NETWORK)
            else -> PendingExecutionOutcome.Retry(ERROR_LOCAL_FAILURE)
        }

    private fun LibraryRemoteException.toLibraryExecutionOutcome(
        operation: PendingSyncOperationEntity,
    ): PendingExecutionOutcome = if (
        error is DomainError.NotFound &&
        operation.operationType == SyncOperationType.REMOVE_FAVORITE
    ) {
        PendingExecutionOutcome.Success
    } else {
        error.toOutcome()
    }

    private fun DomainError.toOutcome(): PendingExecutionOutcome = when (this) {
        is DomainError.RateLimited -> PendingExecutionOutcome.Retry("RATE_LIMITED")
        is DomainError.ServiceUnavailable -> PendingExecutionOutcome.Retry("SERVICE_UNAVAILABLE")
        is DomainError.Server -> PendingExecutionOutcome.Retry("SERVER_ERROR")
        is DomainError.Network -> PendingExecutionOutcome.Retry(ERROR_NETWORK)
        is DomainError.Authentication -> PendingExecutionOutcome.Retry(reason.toWireValue())
        is DomainError.Conflict -> PendingExecutionOutcome.Conflict(reason.toWireValue())
        is DomainError.Validation -> PendingExecutionOutcome.Conflict(reason.toWireValue())
        is DomainError.PermissionDenied -> PendingExecutionOutcome.Conflict(reason.toWireValue())
        is DomainError.NotFound -> PendingExecutionOutcome.Conflict("RESOURCE_NOT_FOUND")
        is DomainError.Protocol -> PendingExecutionOutcome.Conflict(ERROR_PROTOCOL)
        is DomainError.Local -> PendingExecutionOutcome.Conflict("LOCAL_DATA_ERROR")
    }

    private companion object {
        const val ERROR_NETWORK = "NETWORK_ERROR"
        const val ERROR_LOCAL_FAILURE = "LOCAL_SYNC_FAILURE"
        const val ERROR_INVALID_PAYLOAD = "INVALID_PENDING_PAYLOAD"
        const val ERROR_PROTOCOL = "PROTOCOL_ERROR"
    }
}
