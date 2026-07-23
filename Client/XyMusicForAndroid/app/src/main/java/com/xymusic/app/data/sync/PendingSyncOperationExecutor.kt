package com.xymusic.app.data.sync

import com.xymusic.app.core.data.media.CatalogLocalDataSource
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.dao.LibraryDao
import com.xymusic.app.core.database.dao.PendingSyncOperationDao
import com.xymusic.app.core.database.entity.PendingSyncOperationEntity
import com.xymusic.app.core.database.model.SyncOperationType
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.feature.library.data.remote.LibraryProtocolException
import com.xymusic.app.feature.library.data.remote.LibraryRemoteDataSource
import com.xymusic.app.feature.library.data.remote.LibraryRemoteException
import java.io.IOException
import java.time.Clock
import java.util.concurrent.CancellationException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json

sealed interface PendingExecutionOutcome {
    data object Success : PendingExecutionOutcome

    data object OwnerChanged : PendingExecutionOutcome

    data class Retry(val errorCode: String) : PendingExecutionOutcome

    data class Conflict(val errorCode: String) : PendingExecutionOutcome
}

@Singleton
class PendingSyncOperationExecutor
@Inject
constructor(
    database: XyMusicDatabase,
    libraryDao: LibraryDao,
    pendingDao: PendingSyncOperationDao,
    catalogLocal: CatalogLocalDataSource,
    libraryRemote: LibraryRemoteDataSource,
    sessionProvider: AppSessionProvider,
    sessionMutationCoordinator: SessionMutationCoordinator,
    json: Json,
    clock: Clock,
) {
    private val ownerGuard = PendingSyncOwnerGuard(sessionProvider, sessionMutationCoordinator)
    private val payloadCodec = PendingSyncPayloadCodec(json)
    private val operationStore = PendingSyncOperationStore(pendingDao)
    private val libraryStore =
        LibraryPendingSyncStore(
            database = database,
            libraryDao = libraryDao,
            catalogLocal = catalogLocal,
            operationStore = operationStore,
            clock = clock,
        )
    private val libraryHandler =
        LibraryPendingSyncHandler(
            libraryRemote = libraryRemote,
            ownerGuard = ownerGuard,
            payloadCodec = payloadCodec,
            libraryStore = libraryStore,
        )

    suspend fun execute(operation: PendingSyncOperationEntity): PendingExecutionOutcome = try {
        executeOperation(operation)
    } catch (failure: CancellationException) {
        throw failure
    } catch (failure: Exception) {
        failure.toExecutionOutcome(operation)
    }

    private suspend fun executeOperation(operation: PendingSyncOperationEntity): PendingExecutionOutcome {
        ownerGuard.ensureActive(operation.ownerUserId)
        return when (operation.operationType) {
            SyncOperationType.ADD_FAVORITE -> libraryHandler.addFavorite(operation)
            SyncOperationType.REMOVE_FAVORITE -> libraryHandler.removeFavorite(operation)
            SyncOperationType.RECORD_PLAYBACK -> libraryHandler.recordPlayback(operation)
            SyncOperationType.CREATE_PLAYLIST,
            SyncOperationType.UPDATE_PLAYLIST,
            SyncOperationType.DELETE_PLAYLIST,
            SyncOperationType.ADD_PLAYLIST_ENTRY,
            SyncOperationType.REMOVE_PLAYLIST_ENTRY,
            SyncOperationType.REORDER_PLAYLIST_ENTRIES,
            -> PendingExecutionOutcome.Success
        }
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
        is DomainError.Authentication -> PendingExecutionOutcome.Retry(reason.wireValue)
        is DomainError.Conflict -> PendingExecutionOutcome.Conflict(reason.wireValue)
        is DomainError.Validation -> PendingExecutionOutcome.Conflict(reason.wireValue)
        is DomainError.PermissionDenied -> PendingExecutionOutcome.Conflict(reason.wireValue)
        is DomainError.NotFound -> PendingExecutionOutcome.Conflict("RESOURCE_NOT_FOUND")
        is DomainError.Protocol -> PendingExecutionOutcome.Conflict("PROTOCOL_ERROR")
        is DomainError.Local -> PendingExecutionOutcome.Conflict("LOCAL_DATA_ERROR")
    }

    private companion object {
        const val ERROR_NETWORK = "NETWORK_ERROR"
        const val ERROR_LOCAL_FAILURE = "LOCAL_SYNC_FAILURE"
        const val ERROR_INVALID_PAYLOAD = "INVALID_PENDING_PAYLOAD"
        const val ERROR_PROTOCOL = "PROTOCOL_ERROR"
    }
}
