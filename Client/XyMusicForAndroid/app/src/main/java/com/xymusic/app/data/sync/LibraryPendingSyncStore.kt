package com.xymusic.app.data.sync

import androidx.room.withTransaction
import com.xymusic.app.core.data.media.CatalogLocalDataSource
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.dao.LibraryDao
import com.xymusic.app.core.database.entity.FavoriteEntity
import com.xymusic.app.core.database.entity.HistoryEntity
import com.xymusic.app.core.database.entity.PendingSyncOperationEntity
import com.xymusic.app.core.database.model.SyncOperationType
import com.xymusic.app.feature.library.data.remote.FavoriteItemDto
import com.xymusic.app.feature.library.data.remote.HistoryItemDto
import java.time.Clock
import java.time.Instant

internal class LibraryPendingSyncStore(
    private val database: XyMusicDatabase,
    private val libraryDao: LibraryDao,
    private val catalogLocal: CatalogLocalDataSource,
    private val operationStore: PendingSyncOperationStore,
    private val clock: Clock,
) {
    suspend fun persistFavorite(operation: PendingSyncOperationEntity, item: FavoriteItemDto) {
        val laterRemoveExists =
            operationStore.hasLaterOperation(
                operation,
                SyncOperationType.REMOVE_FAVORITE,
            )
        database.withTransaction {
            catalogLocal.mergeTrackSummaries(listOf(item.track), clock.millis())
            if (!laterRemoveExists) {
                libraryDao.upsertFavorite(
                    FavoriteEntity(
                        operation.ownerUserId,
                        item.track.id,
                        Instant.parse(item.favoritedAt).toEpochMilli(),
                    ),
                )
            }
        }
    }

    suspend fun persistHistory(ownerUserId: String, item: HistoryItemDto) {
        val updatedAt = Instant.parse(item.updatedAt).toEpochMilli()
        database.withTransaction {
            catalogLocal.mergeTrackSummaries(listOf(item.track), clock.millis())
            val cached = libraryDao.history(ownerUserId, item.track.id)
            if (cached == null || cached.updatedAtEpochMs <= updatedAt) {
                libraryDao.upsertHistory(
                    HistoryEntity(
                        ownerUserId = ownerUserId,
                        trackId = item.track.id,
                        lastPositionMs = item.lastPositionMs,
                        playCount = item.playCount,
                        lastPlayedAtEpochMs = Instant.parse(item.lastPlayedAt).toEpochMilli(),
                        completed = item.completed,
                        updatedAtEpochMs = updatedAt,
                    ),
                )
            }
        }
    }
}
