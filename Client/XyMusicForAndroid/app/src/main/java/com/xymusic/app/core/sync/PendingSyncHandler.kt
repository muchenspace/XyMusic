package com.xymusic.app.core.sync

import com.xymusic.app.core.database.entity.PendingSyncOperationEntity
import com.xymusic.app.core.database.model.SyncOperationType

sealed interface PendingExecutionOutcome {
    data object Success : PendingExecutionOutcome

    data object OwnerChanged : PendingExecutionOutcome

    data class Retry(val errorCode: String) : PendingExecutionOutcome

    data class Conflict(val errorCode: String) : PendingExecutionOutcome
}

interface PendingSyncOperationHandler {
    val supportedOperationTypes: Set<SyncOperationType>

    suspend fun execute(operation: PendingSyncOperationEntity): PendingExecutionOutcome
}
