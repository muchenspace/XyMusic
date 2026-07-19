package com.xymusic.app.core.database.dao

import androidx.room.Dao
import androidx.room.Query
import androidx.room.Upsert
import com.xymusic.app.core.database.entity.OfflineTrackEntity
import kotlinx.coroutines.flow.Flow

@Dao
interface OfflineTrackDao {
    @Query(
        "SELECT * FROM offline_tracks WHERE owner_user_id = :ownerUserId " +
            "ORDER BY downloaded_at_epoch_ms DESC, track_id DESC",
    )
    fun observeAll(ownerUserId: String): Flow<List<OfflineTrackEntity>>

    @Query(
        "SELECT EXISTS(SELECT 1 FROM offline_tracks " +
            "WHERE owner_user_id = :ownerUserId AND track_id = :trackId)",
    )
    fun observeDownloaded(ownerUserId: String, trackId: String): Flow<Boolean>

    @Query(
        "SELECT * FROM offline_tracks " +
            "WHERE owner_user_id = :ownerUserId AND track_id = :trackId",
    )
    suspend fun track(ownerUserId: String, trackId: String): OfflineTrackEntity?

    @Query("SELECT * FROM offline_tracks WHERE owner_user_id = :ownerUserId")
    suspend fun tracks(ownerUserId: String): List<OfflineTrackEntity>

    @Query("SELECT DISTINCT cache_key FROM offline_tracks")
    fun observeCacheKeys(): Flow<List<String>>

    @Query("SELECT COUNT(*) FROM offline_tracks WHERE cache_key = :cacheKey")
    suspend fun cacheKeyReferenceCount(cacheKey: String): Int

    @Upsert
    suspend fun upsert(track: OfflineTrackEntity)

    @Query(
        "DELETE FROM offline_tracks " +
            "WHERE owner_user_id = :ownerUserId AND track_id = :trackId",
    )
    suspend fun delete(ownerUserId: String, trackId: String): Int

    @Query("DELETE FROM offline_tracks WHERE owner_user_id = :ownerUserId")
    suspend fun deleteOwner(ownerUserId: String): Int

    @Query("DELETE FROM offline_tracks")
    suspend fun clear(): Int
}
