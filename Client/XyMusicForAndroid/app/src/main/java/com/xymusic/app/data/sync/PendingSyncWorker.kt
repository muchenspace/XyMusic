package com.xymusic.app.data.sync

import android.content.Context
import androidx.hilt.work.HiltWorker
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import com.xymusic.app.core.database.dao.PendingSyncOperationDao
import com.xymusic.app.core.database.entity.PendingSyncOperationEntity
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.core.sync.PendingSyncScheduler
import dagger.assisted.Assisted
import dagger.assisted.AssistedInject
import java.time.Clock
import java.util.UUID
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.first

@HiltWorker
class PendingSyncWorker
@AssistedInject
constructor(
    @Assisted appContext: Context,
    @Assisted workerParameters: WorkerParameters,
    private val pendingDao: PendingSyncOperationDao,
    private val executor: PendingSyncOperationExecutor,
    private val sessionProvider: AppSessionProvider,
    private val sessionMutationCoordinator: SessionMutationCoordinator,
    private val pendingSyncScheduler: PendingSyncScheduler,
    private val clock: Clock,
) : CoroutineWorker(appContext, workerParameters) {
    override suspend fun doWork(): Result {
        val ownerUserId = validOwnerUserId() ?: return Result.failure()
        val leaseOwner = id.toString()

        return try {
            drainOperations(ownerUserId, leaseOwner)
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: Exception) {
            Result.retry()
        }
    }

    private fun validOwnerUserId(): String? = inputData
        .getString(KEY_OWNER_USER_ID)
        ?.takeIf { runCatching { UUID.fromString(it) }.isSuccess }

    private suspend fun drainOperations(ownerUserId: String, leaseOwner: String): Result {
        sessionProvider.restoreSession()
        repeat(MAX_OPERATIONS_PER_RUN) {
            processNextOperation(ownerUserId, leaseOwner)?.let { return it }
        }
        return finishDrain(ownerUserId)
    }

    private suspend fun processNextOperation(ownerUserId: String, leaseOwner: String): Result? {
        if (!isActiveOwner(ownerUserId)) return Result.success()
        val operation =
            pendingDao.claimNext(
                ownerUserId = ownerUserId,
                leaseOwner = leaseOwner,
                nowEpochMs = clock.millis(),
                leaseDurationMs = LEASE_DURATION_MS,
            ) ?: return resultForEmptyClaim(ownerUserId)
        val outcome = executor.execute(operation)
        return persistOutcome(ownerUserId, leaseOwner, operation, outcome)
    }

    private suspend fun resultForEmptyClaim(ownerUserId: String): Result =
        if (hasActionableOperations(ownerUserId)) Result.retry() else Result.success()

    private suspend fun persistOutcome(
        ownerUserId: String,
        leaseOwner: String,
        operation: PendingSyncOperationEntity,
        outcome: PendingExecutionOutcome,
    ): Result? = sessionMutationCoordinator.mutate {
        if (!isActiveOwner(ownerUserId)) return@mutate Result.success()
        when (outcome) {
            PendingExecutionOutcome.OwnerChanged -> Result.success()
            PendingExecutionOutcome.Success -> completeOperation(ownerUserId, operation.id, leaseOwner)
            is PendingExecutionOutcome.Conflict ->
                markConflict(ownerUserId, operation.id, leaseOwner, outcome.errorCode)
            is PendingExecutionOutcome.Retry -> {
                markFailed(ownerUserId, operation, leaseOwner, outcome.errorCode)
                Result.retry()
            }
        }
    }

    private suspend fun completeOperation(ownerUserId: String, operationId: String, leaseOwner: String): Result? =
        if (pendingDao.complete(ownerUserId, operationId, leaseOwner) == 1) null else Result.retry()

    private suspend fun markConflict(
        ownerUserId: String,
        operationId: String,
        leaseOwner: String,
        errorCode: String,
    ): Result? = if (
        pendingDao.markConflict(
            ownerUserId,
            operationId,
            leaseOwner,
            errorCode,
            clock.millis(),
        ) == 1
    ) {
        null
    } else {
        Result.retry()
    }

    private suspend fun markFailed(
        ownerUserId: String,
        operation: PendingSyncOperationEntity,
        leaseOwner: String,
        errorCode: String,
    ) {
        val now = clock.millis()
        val nextAttempt = Math.addExact(now, retryDelayMs(operation.attemptCount))
        pendingDao.markFailed(
            ownerUserId,
            operation.id,
            leaseOwner,
            errorCode,
            now,
            nextAttempt,
        )
    }

    private suspend fun finishDrain(ownerUserId: String): Result {
        if (hasActionableOperations(ownerUserId)) {
            pendingSyncScheduler.continueDrain(ownerUserId)
        }
        return Result.success()
    }

    private fun isActiveOwner(ownerUserId: String): Boolean =
        (sessionProvider.sessionState.value as? AppSessionState.SignedIn)?.userId == ownerUserId

    private suspend fun hasActionableOperations(ownerUserId: String): Boolean =
        pendingDao.observeActionableCount(ownerUserId).first() > 0

    private fun retryDelayMs(attemptCount: Int): Long {
        val exponent = (attemptCount - 1).coerceIn(0, MAX_BACKOFF_EXPONENT)
        return (INITIAL_RETRY_DELAY_MS shl exponent).coerceAtMost(MAX_RETRY_DELAY_MS)
    }

    companion object {
        const val KEY_OWNER_USER_ID = "owner_user_id"
        private const val MAX_OPERATIONS_PER_RUN = 100
        private const val LEASE_DURATION_MS = 2 * 60 * 1_000L
        private const val INITIAL_RETRY_DELAY_MS = 15_000L
        private const val MAX_RETRY_DELAY_MS = 6 * 60 * 60 * 1_000L
        private const val MAX_BACKOFF_EXPONENT = 10
    }
}
