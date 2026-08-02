package com.xymusic.app.data.sync

import com.xymusic.app.core.database.entity.PendingSyncOperationEntity
import com.xymusic.app.core.sync.PendingExecutionOutcome
import com.xymusic.app.core.sync.PendingSyncOperationHandler
import com.xymusic.app.core.sync.PendingSyncOwnerChangedException
import com.xymusic.app.core.sync.PendingSyncOwnerGuard
import java.util.concurrent.CancellationException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.serialization.SerializationException

@Singleton
class PendingSyncOperationExecutor
@Inject
constructor(
    private val ownerGuard: PendingSyncOwnerGuard,
    private val handlers: Set<@JvmSuppressWildcards PendingSyncOperationHandler>,
) {
    suspend fun execute(operation: PendingSyncOperationEntity): PendingExecutionOutcome = try {
        ownerGuard.ensureActive(operation.ownerUserId)
        handlers
            .firstOrNull { operation.operationType in it.supportedOperationTypes }
            ?.execute(operation)
            ?: PendingExecutionOutcome.Success
    } catch (failure: CancellationException) {
        throw failure
    } catch (failure: Exception) {
        when (failure) {
            is PendingSyncOwnerChangedException -> PendingExecutionOutcome.OwnerChanged
            is SerializationException,
            is IllegalArgumentException,
            -> PendingExecutionOutcome.Conflict(ERROR_INVALID_PAYLOAD)
            else -> PendingExecutionOutcome.Retry(ERROR_LOCAL_FAILURE)
        }
    }

    private companion object {
        const val ERROR_LOCAL_FAILURE = "LOCAL_SYNC_FAILURE"
        const val ERROR_INVALID_PAYLOAD = "INVALID_PENDING_PAYLOAD"
    }
}
