package com.xymusic.app.core.database.dao

import androidx.room.Dao
import androidx.room.Query
import androidx.room.Transaction

data class AccountDataDeletion(
    val favoriteCount: Int,
    val historyCount: Int,
    val playlistEntryCount: Int,
    val playlistCount: Int,
    val queueItemCount: Int,
    val searchHistoryCount: Int,
    val pendingSyncCount: Int,
    val offlineTrackCount: Int = 0,
) {
    val totalCount: Int
        get() =
            favoriteCount + historyCount + playlistEntryCount + playlistCount +
                queueItemCount + searchHistoryCount + pendingSyncCount + offlineTrackCount
}

@Dao
abstract class AccountDataDao {
    @Query("DELETE FROM pending_sync_operations WHERE owner_user_id = :ownerUserId")
    protected abstract suspend fun deletePendingSync(ownerUserId: String): Int

    @Query("DELETE FROM playback_queue WHERE owner_user_id = :ownerUserId")
    protected abstract suspend fun deleteQueue(ownerUserId: String): Int

    @Query("DELETE FROM search_history WHERE owner_user_id = :ownerUserId")
    protected abstract suspend fun deleteSearchHistory(ownerUserId: String): Int

    @Query("DELETE FROM playlist_entries WHERE owner_user_id = :ownerUserId")
    protected abstract suspend fun deletePlaylistEntries(ownerUserId: String): Int

    @Query("DELETE FROM playlists WHERE owner_user_id = :ownerUserId")
    protected abstract suspend fun deletePlaylists(ownerUserId: String): Int

    @Query("DELETE FROM favorites WHERE owner_user_id = :ownerUserId")
    protected abstract suspend fun deleteFavorites(ownerUserId: String): Int

    @Query("DELETE FROM playback_history WHERE owner_user_id = :ownerUserId")
    protected abstract suspend fun deleteHistory(ownerUserId: String): Int

    @Transaction
    open suspend fun deletePrivateData(ownerUserId: String): AccountDataDeletion {
        require(ownerUserId.isNotBlank()) { "Owner user ID cannot be blank" }
        val pendingSyncCount = deletePendingSync(ownerUserId)
        val queueItemCount = deleteQueue(ownerUserId)
        val searchHistoryCount = deleteSearchHistory(ownerUserId)
        val playlistEntryCount = deletePlaylistEntries(ownerUserId)
        val playlistCount = deletePlaylists(ownerUserId)
        val favoriteCount = deleteFavorites(ownerUserId)
        val historyCount = deleteHistory(ownerUserId)
        return AccountDataDeletion(
            favoriteCount = favoriteCount,
            historyCount = historyCount,
            playlistEntryCount = playlistEntryCount,
            playlistCount = playlistCount,
            queueItemCount = queueItemCount,
            searchHistoryCount = searchHistoryCount,
            pendingSyncCount = pendingSyncCount,
        )
    }
}
