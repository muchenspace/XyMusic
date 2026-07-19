package com.xymusic.app.core.database.entity

import androidx.room.ColumnInfo
import androidx.room.Entity
import androidx.room.Index
import com.xymusic.app.core.database.model.CatalogItemType
import com.xymusic.app.core.database.model.SyncOperationStatus
import com.xymusic.app.core.database.model.SyncOperationType
import com.xymusic.app.core.database.model.SyncTargetType

@Entity(
    tableName = "catalog_remote_keys",
    primaryKeys = ["collection_key", "item_type", "item_id"],
    indices = [
        Index(
            name = "index_catalog_remote_keys_position",
            value = ["collection_key", "item_type", "position"],
            unique = true,
        ),
        Index(name = "index_catalog_remote_keys_item", value = ["item_type", "item_id"]),
    ],
)
data class CatalogRemoteKeyEntity(
    @ColumnInfo(name = "collection_key") val collectionKey: String,
    @ColumnInfo(name = "item_type") val itemType: CatalogItemType,
    @ColumnInfo(name = "item_id") val itemId: String,
    @ColumnInfo(name = "position") val position: Long,
    @ColumnInfo(name = "previous_cursor") val previousCursor: String?,
    @ColumnInfo(name = "next_cursor") val nextCursor: String?,
    @ColumnInfo(name = "refreshed_at_epoch_ms") val refreshedAtEpochMs: Long,
)

@Entity(
    tableName = "pending_sync_operations",
    primaryKeys = ["owner_user_id", "id"],
    indices = [
        Index(
            name = "index_pending_sync_owner_idempotency",
            value = ["owner_user_id", "idempotency_key"],
            unique = true,
        ),
        Index(
            name = "index_pending_sync_owner_ready",
            value = ["owner_user_id", "status", "next_attempt_at_epoch_ms", "created_at_epoch_ms"],
        ),
        Index(name = "index_pending_sync_lease", value = ["status", "lease_expires_at_epoch_ms"]),
    ],
)
data class PendingSyncOperationEntity(
    @ColumnInfo(name = "owner_user_id") val ownerUserId: String,
    @ColumnInfo(name = "id") val id: String,
    @ColumnInfo(name = "operation_type") val operationType: SyncOperationType,
    @ColumnInfo(name = "target_type") val targetType: SyncTargetType,
    @ColumnInfo(name = "target_id") val targetId: String?,
    // Payloads contain only business mutation fields; credentials and temporary media URLs are forbidden.
    @ColumnInfo(name = "request_payload_json") val requestPayloadJson: String?,
    @ColumnInfo(name = "idempotency_key") val idempotencyKey: String,
    @ColumnInfo(name = "status") val status: SyncOperationStatus,
    @ColumnInfo(name = "attempt_count") val attemptCount: Int,
    @ColumnInfo(name = "created_at_epoch_ms") val createdAtEpochMs: Long,
    @ColumnInfo(name = "updated_at_epoch_ms") val updatedAtEpochMs: Long,
    @ColumnInfo(name = "next_attempt_at_epoch_ms") val nextAttemptAtEpochMs: Long,
    @ColumnInfo(name = "lease_owner") val leaseOwner: String?,
    @ColumnInfo(name = "lease_expires_at_epoch_ms") val leaseExpiresAtEpochMs: Long?,
    @ColumnInfo(name = "last_error_code") val lastErrorCode: String?,
)
