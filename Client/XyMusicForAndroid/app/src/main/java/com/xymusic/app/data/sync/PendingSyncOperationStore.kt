package com.xymusic.app.data.sync

import com.xymusic.app.core.database.dao.PendingSyncOperationDao
import com.xymusic.app.core.database.entity.PendingSyncOperationEntity
import com.xymusic.app.core.database.model.SyncOperationStatus
import com.xymusic.app.core.database.model.SyncOperationType

internal class PendingSyncOperationStore(private val pendingDao: PendingSyncOperationDao) {
    suspend fun hasLaterOperation(current: PendingSyncOperationEntity, operationType: SyncOperationType): Boolean =
        laterOperations(current).any { it.operationType == operationType }

    private suspend fun laterOperations(current: PendingSyncOperationEntity): List<PendingSyncOperationEntity> {
        val targetId = current.targetId ?: return emptyList()
        return pendingDao.forTarget(current.ownerUserId, current.targetType, targetId).filter {
            it.id != current.id &&
                it.status in setOf(SyncOperationStatus.PENDING, SyncOperationStatus.FAILED) &&
                (
                    it.createdAtEpochMs > current.createdAtEpochMs ||
                        it.createdAtEpochMs == current.createdAtEpochMs &&
                        it.id > current.id
                    )
        }
    }
}
