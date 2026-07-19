package com.xymusic.app.data.sync

import com.xymusic.app.core.database.entity.PendingSyncOperationEntity
import com.xymusic.app.feature.library.data.remote.LibraryRemoteDataSource
import com.xymusic.app.feature.library.data.remote.RecordPlaybackRequestDto
import com.xymusic.app.feature.library.data.sync.FavoritePendingPayload
import com.xymusic.app.feature.library.data.sync.PlaybackPendingPayload

internal class LibraryPendingSyncHandler(
    private val libraryRemote: LibraryRemoteDataSource,
    private val ownerGuard: PendingSyncOwnerGuard,
    private val payloadCodec: PendingSyncPayloadCodec,
    private val libraryStore: LibraryPendingSyncStore,
) {
    suspend fun addFavorite(operation: PendingSyncOperationEntity): PendingExecutionOutcome {
        val payload = payloadCodec.decode(operation, FavoritePendingPayload.serializer())
        requireTarget(operation, payload.trackId)
        val item = libraryRemote.addFavorite(payload.trackId)
        ownerGuard.mutateIfActive(operation.ownerUserId) {
            libraryStore.persistFavorite(operation, item)
        }
        return PendingExecutionOutcome.Success
    }

    suspend fun removeFavorite(operation: PendingSyncOperationEntity): PendingExecutionOutcome {
        val payload = payloadCodec.decode(operation, FavoritePendingPayload.serializer())
        requireTarget(operation, payload.trackId)
        libraryRemote.removeFavorite(payload.trackId)
        ownerGuard.mutateIfActive(operation.ownerUserId) { Unit }
        return PendingExecutionOutcome.Success
    }

    suspend fun recordPlayback(operation: PendingSyncOperationEntity): PendingExecutionOutcome {
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
}
