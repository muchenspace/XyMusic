package com.xymusic.app.core.database.dao

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import androidx.room.Transaction
import com.xymusic.app.core.database.entity.PendingSyncOperationEntity
import com.xymusic.app.core.database.model.SyncOperationStatus
import com.xymusic.app.core.database.model.SyncOperationType
import com.xymusic.app.core.database.model.SyncTargetType
import kotlinx.coroutines.flow.Flow

@Dao
abstract class PendingSyncOperationDao {
    @Insert(onConflict = OnConflictStrategy.IGNORE)
    abstract suspend fun enqueue(operation: PendingSyncOperationEntity): Long

    @Query(
        """
        SELECT * FROM pending_sync_operations
        WHERE owner_user_id = :ownerUserId
          AND operation_type = 'RECORD_PLAYBACK'
          AND status = 'PENDING'
          AND attempt_count = 0
          AND substr(idempotency_key, 1, length(:idempotencyKeyPrefix)) = :idempotencyKeyPrefix
        ORDER BY created_at_epoch_ms DESC, id DESC
        LIMIT 1
        """,
    )
    protected abstract suspend fun replaceablePlaybackProgress(
        ownerUserId: String,
        idempotencyKeyPrefix: String,
    ): PendingSyncOperationEntity?

    @Query(
        """
        UPDATE pending_sync_operations
        SET request_payload_json = :requestPayloadJson,
            updated_at_epoch_ms = :nowEpochMs,
            next_attempt_at_epoch_ms = :nowEpochMs,
            last_error_code = NULL
        WHERE owner_user_id = :ownerUserId
          AND id = :operationId
          AND status = 'PENDING'
          AND attempt_count = 0
        """,
    )
    protected abstract suspend fun replaceUnattemptedPlaybackProgress(
        ownerUserId: String,
        operationId: String,
        requestPayloadJson: String,
        nowEpochMs: Long,
    ): Int

    @Transaction
    open suspend fun enqueueOrReplacePlaybackProgress(
        operation: PendingSyncOperationEntity,
        idempotencyKeyPrefix: String,
    ) {
        require(operation.operationType == SyncOperationType.RECORD_PLAYBACK) {
            "Only playback operations can be coalesced"
        }
        require(operation.status == SyncOperationStatus.PENDING && operation.attemptCount == 0) {
            "A new playback progress operation must be unattempted and pending"
        }
        require(operation.idempotencyKey.startsWith(idempotencyKeyPrefix)) {
            "Playback progress idempotency key does not match its coalescing prefix"
        }
        val payload =
            requireNotNull(operation.requestPayloadJson) {
                "Playback progress payload cannot be null"
            }
        val existing = replaceablePlaybackProgress(operation.ownerUserId, idempotencyKeyPrefix)
        if (
            existing != null &&
            replaceUnattemptedPlaybackProgress(
                ownerUserId = operation.ownerUserId,
                operationId = existing.id,
                requestPayloadJson = payload,
                nowEpochMs = operation.updatedAtEpochMs,
            ) == 1
        ) {
            return
        }
        check(enqueue(operation) != INSERT_IGNORED) {
            "Unable to enqueue playback progress"
        }
    }

    @Query(
        """
        SELECT * FROM pending_sync_operations
        WHERE owner_user_id = :ownerUserId
          AND (
              (status IN ('PENDING', 'FAILED') AND next_attempt_at_epoch_ms <= :nowEpochMs)
              OR (status = 'RUNNING' AND lease_expires_at_epoch_ms <= :nowEpochMs)
          )
        ORDER BY next_attempt_at_epoch_ms, created_at_epoch_ms, id
        LIMIT 1
        """,
    )
    protected abstract suspend fun nextClaimable(ownerUserId: String, nowEpochMs: Long): PendingSyncOperationEntity?

    @Query(
        """
        UPDATE pending_sync_operations
        SET status = 'RUNNING',
            attempt_count = attempt_count + 1,
            updated_at_epoch_ms = :nowEpochMs,
            lease_owner = :leaseOwner,
            lease_expires_at_epoch_ms = :leaseExpiresAtEpochMs,
            last_error_code = NULL
        WHERE owner_user_id = :ownerUserId
          AND id = :operationId
          AND (
              (status IN ('PENDING', 'FAILED') AND next_attempt_at_epoch_ms <= :nowEpochMs)
              OR (status = 'RUNNING' AND lease_expires_at_epoch_ms <= :nowEpochMs)
          )
        """,
    )
    protected abstract suspend fun claim(
        ownerUserId: String,
        operationId: String,
        leaseOwner: String,
        nowEpochMs: Long,
        leaseExpiresAtEpochMs: Long,
    ): Int

    @Transaction
    open suspend fun claimNext(
        ownerUserId: String,
        leaseOwner: String,
        nowEpochMs: Long,
        leaseDurationMs: Long,
    ): PendingSyncOperationEntity? {
        require(leaseOwner.isNotBlank()) { "Lease owner cannot be blank" }
        require(leaseDurationMs > 0) { "Lease duration must be positive" }
        val candidate = nextClaimable(ownerUserId, nowEpochMs) ?: return null
        val leaseExpiresAtEpochMs = Math.addExact(nowEpochMs, leaseDurationMs)
        val updated =
            claim(
                ownerUserId = ownerUserId,
                operationId = candidate.id,
                leaseOwner = leaseOwner,
                nowEpochMs = nowEpochMs,
                leaseExpiresAtEpochMs = leaseExpiresAtEpochMs,
            )
        return if (updated == 1) {
            candidate.copy(
                status = SyncOperationStatus.RUNNING,
                attemptCount = candidate.attemptCount + 1,
                updatedAtEpochMs = nowEpochMs,
                leaseOwner = leaseOwner,
                leaseExpiresAtEpochMs = leaseExpiresAtEpochMs,
                lastErrorCode = null,
            )
        } else {
            null
        }
    }

    @Query(
        """
        DELETE FROM pending_sync_operations
        WHERE owner_user_id = :ownerUserId
          AND id = :operationId
          AND status = 'RUNNING'
          AND lease_owner = :leaseOwner
        """,
    )
    abstract suspend fun complete(ownerUserId: String, operationId: String, leaseOwner: String): Int

    @Query(
        """
        UPDATE pending_sync_operations
        SET status = 'FAILED',
            updated_at_epoch_ms = :nowEpochMs,
            next_attempt_at_epoch_ms = :nextAttemptAtEpochMs,
            lease_owner = NULL,
            lease_expires_at_epoch_ms = NULL,
            last_error_code = :errorCode
        WHERE owner_user_id = :ownerUserId
          AND id = :operationId
          AND status = 'RUNNING'
          AND lease_owner = :leaseOwner
        """,
    )
    abstract suspend fun markFailed(
        ownerUserId: String,
        operationId: String,
        leaseOwner: String,
        errorCode: String,
        nowEpochMs: Long,
        nextAttemptAtEpochMs: Long,
    ): Int

    @Query(
        """
        UPDATE pending_sync_operations
        SET status = 'CONFLICT',
            updated_at_epoch_ms = :nowEpochMs,
            lease_owner = NULL,
            lease_expires_at_epoch_ms = NULL,
            last_error_code = :errorCode
        WHERE owner_user_id = :ownerUserId
          AND id = :operationId
          AND status = 'RUNNING'
          AND lease_owner = :leaseOwner
        """,
    )
    abstract suspend fun markConflict(
        ownerUserId: String,
        operationId: String,
        leaseOwner: String,
        errorCode: String,
        nowEpochMs: Long,
    ): Int

    @Query(
        """
        SELECT * FROM pending_sync_operations
        WHERE owner_user_id = :ownerUserId
        ORDER BY created_at_epoch_ms, id
        """,
    )
    abstract fun observeAll(ownerUserId: String): Flow<List<PendingSyncOperationEntity>>

    @Query(
        """
        SELECT * FROM pending_sync_operations
        WHERE owner_user_id = :ownerUserId
          AND target_type = :targetType
          AND target_id = :targetId
        ORDER BY created_at_epoch_ms, id
        """,
    )
    abstract suspend fun forTarget(
        ownerUserId: String,
        targetType: SyncTargetType,
        targetId: String,
    ): List<PendingSyncOperationEntity>

    @Query(
        """
        SELECT * FROM pending_sync_operations
        WHERE owner_user_id = :ownerUserId
          AND target_type = :targetType
          AND status != 'CONFLICT'
        ORDER BY created_at_epoch_ms, id
        """,
    )
    abstract suspend fun actionableForTargetType(
        ownerUserId: String,
        targetType: SyncTargetType,
    ): List<PendingSyncOperationEntity>

    @Query(
        """
        SELECT * FROM pending_sync_operations
        WHERE owner_user_id = :ownerUserId
          AND target_type = :targetType
          AND target_id IN (:targetIds)
          AND status != 'CONFLICT'
        ORDER BY created_at_epoch_ms, id
        """,
    )
    abstract suspend fun actionableForTargets(
        ownerUserId: String,
        targetType: SyncTargetType,
        targetIds: List<String>,
    ): List<PendingSyncOperationEntity>

    @Query(
        """
        SELECT COUNT(*) FROM pending_sync_operations
        WHERE owner_user_id = :ownerUserId AND status != 'CONFLICT'
        """,
    )
    abstract fun observeActionableCount(ownerUserId: String): Flow<Int>

    private companion object {
        const val INSERT_IGNORED = -1L
    }
}
