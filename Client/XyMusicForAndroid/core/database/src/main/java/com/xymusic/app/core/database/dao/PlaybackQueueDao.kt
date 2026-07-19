package com.xymusic.app.core.database.dao

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import androidx.room.Transaction
import com.xymusic.app.core.database.entity.PlaybackQueueEntity
import kotlinx.coroutines.flow.Flow

@Dao
abstract class PlaybackQueueDao {
    @Insert(onConflict = OnConflictStrategy.ABORT)
    protected abstract suspend fun insertItems(items: List<PlaybackQueueEntity>)

    @Query("DELETE FROM playback_queue WHERE owner_user_id = :ownerUserId")
    abstract suspend fun clear(ownerUserId: String): Int

    @Transaction
    open suspend fun replace(ownerUserId: String, items: List<PlaybackQueueEntity>) {
        require(ownerUserId.isNotBlank()) { "Queue owner must not be blank" }
        require(items.all { it.ownerUserId == ownerUserId }) { "Queue item belongs to another owner" }
        require(items.map { it.itemId }.distinct().size == items.size) {
            "Queue item IDs must be unique"
        }
        require(items.all { it.itemId.isNotBlank() && it.trackId.isNotBlank() }) {
            "Queue item and track IDs must not be blank"
        }
        require(items.all { it.resumePositionMs >= 0 && it.enqueuedAtEpochMs >= 0 }) {
            "Queue timestamps and resume positions must not be negative"
        }
        require(items.none { it.stableCacheKey.looksLikeNetworkUrl() }) {
            "Queue snapshots must not persist playback URLs"
        }
        items.sortedBy(PlaybackQueueEntity::position).forEachIndexed { index, item ->
            require(item.position == index) { "Queue positions must be contiguous from zero" }
        }
        require(items.isEmpty() || items.count(PlaybackQueueEntity::isCurrent) == 1) {
            "A non-empty queue must have exactly one current item"
        }
        clear(ownerUserId)
        if (items.isNotEmpty()) insertItems(items)
    }

    @Query("SELECT * FROM playback_queue WHERE owner_user_id = :ownerUserId ORDER BY position, item_id")
    abstract fun observe(ownerUserId: String): Flow<List<PlaybackQueueEntity>>

    @Query("SELECT * FROM playback_queue WHERE owner_user_id = :ownerUserId ORDER BY position, item_id")
    abstract suspend fun snapshot(ownerUserId: String): List<PlaybackQueueEntity>

    @Query("SELECT * FROM playback_queue WHERE owner_user_id = :ownerUserId AND is_current = 1 LIMIT 1")
    abstract suspend fun current(ownerUserId: String): PlaybackQueueEntity?

    @Query(
        """
        UPDATE playback_queue
        SET resume_position_ms = :resumePositionMs
        WHERE owner_user_id = :ownerUserId AND item_id = :itemId
        """,
    )
    protected abstract suspend fun updateResumePositionIfPresent(
        ownerUserId: String,
        itemId: String,
        resumePositionMs: Long,
    ): Int

    @Transaction
    open suspend fun updateResumePosition(ownerUserId: String, itemId: String, resumePositionMs: Long) {
        require(ownerUserId.isNotBlank() && itemId.isNotBlank()) {
            "Queue owner and item IDs must not be blank"
        }
        require(resumePositionMs >= 0) { "Resume position must not be negative" }
        check(updateResumePositionIfPresent(ownerUserId, itemId, resumePositionMs) == 1) {
            "Queue item does not exist for the requested owner"
        }
    }

    @Query("UPDATE playback_queue SET is_current = 0 WHERE owner_user_id = :ownerUserId")
    protected abstract suspend fun clearCurrent(ownerUserId: String): Int

    @Query(
        """
        UPDATE playback_queue
        SET is_current = 1,
            resume_position_ms = :resumePositionMs
        WHERE owner_user_id = :ownerUserId AND item_id = :itemId
        """,
    )
    protected abstract suspend fun setCurrentIfPresent(
        ownerUserId: String,
        itemId: String,
        resumePositionMs: Long,
    ): Int

    @Transaction
    open suspend fun setCurrent(ownerUserId: String, itemId: String, resumePositionMs: Long) {
        require(ownerUserId.isNotBlank() && itemId.isNotBlank()) {
            "Queue owner and item IDs must not be blank"
        }
        require(resumePositionMs >= 0) { "Resume position must not be negative" }
        clearCurrent(ownerUserId)
        check(setCurrentIfPresent(ownerUserId, itemId, resumePositionMs) == 1) {
            "Queue item does not exist for the requested owner"
        }
    }

    private fun String?.looksLikeNetworkUrl(): Boolean {
        val normalized = this?.trim()?.lowercase() ?: return false
        return normalized.startsWith("http://") || normalized.startsWith("https://")
    }
}
