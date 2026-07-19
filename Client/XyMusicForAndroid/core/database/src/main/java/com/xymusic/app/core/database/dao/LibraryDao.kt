package com.xymusic.app.core.database.dao

import androidx.paging.PagingSource
import androidx.room.Dao
import androidx.room.Query
import androidx.room.Transaction
import androidx.room.Upsert
import com.xymusic.app.core.database.entity.FavoriteEntity
import com.xymusic.app.core.database.entity.HistoryEntity
import com.xymusic.app.core.database.model.PlaybackHistoryReadModel
import com.xymusic.app.core.database.model.TrackSummaryReadModel
import kotlinx.coroutines.flow.Flow

@Dao
interface LibraryDao {
    @Upsert
    suspend fun upsertFavorite(favorite: FavoriteEntity)

    @Query("SELECT * FROM favorites WHERE owner_user_id = :ownerUserId AND track_id = :trackId")
    suspend fun favorite(ownerUserId: String, trackId: String): FavoriteEntity?

    @Query("SELECT * FROM favorites WHERE owner_user_id = :ownerUserId")
    suspend fun favorites(ownerUserId: String): List<FavoriteEntity>

    @Query("DELETE FROM favorites WHERE owner_user_id = :ownerUserId AND track_id = :trackId")
    suspend fun deleteFavorite(ownerUserId: String, trackId: String): Int

    @Query("DELETE FROM favorites WHERE owner_user_id = :ownerUserId")
    suspend fun deleteFavorites(ownerUserId: String): Int

    @Query(
        "SELECT EXISTS(SELECT 1 FROM favorites WHERE owner_user_id = :ownerUserId AND track_id = :trackId)",
    )
    fun observeFavorite(ownerUserId: String, trackId: String): Flow<Boolean>

    @Transaction
    @Query(
        """
        SELECT tracks.* FROM tracks
        INNER JOIN favorites ON favorites.track_id = tracks.id
        WHERE favorites.owner_user_id = :ownerUserId
        ORDER BY favorites.favorited_at_epoch_ms DESC, favorites.track_id DESC
        """,
    )
    fun pagedFavoriteTracks(ownerUserId: String): PagingSource<Int, TrackSummaryReadModel>

    @Upsert
    suspend fun upsertHistory(history: HistoryEntity)

    @Upsert
    suspend fun upsertHistories(histories: List<HistoryEntity>)

    @Query("SELECT * FROM playback_history WHERE owner_user_id = :ownerUserId AND track_id = :trackId")
    suspend fun history(ownerUserId: String, trackId: String): HistoryEntity?

    @Query(
        "SELECT * FROM playback_history WHERE owner_user_id = :ownerUserId AND track_id IN (:trackIds)",
    )
    suspend fun histories(ownerUserId: String, trackIds: List<String>): List<HistoryEntity>

    @Query(
        """
        SELECT * FROM playback_history
        WHERE owner_user_id = :ownerUserId
        ORDER BY last_played_at_epoch_ms DESC, track_id DESC
        """,
    )
    fun observeHistory(ownerUserId: String): Flow<List<HistoryEntity>>

    @Transaction
    @Query(
        """
        SELECT tracks.*,
               playback_history.last_position_ms AS history_last_position_ms,
               playback_history.play_count AS history_play_count,
               playback_history.last_played_at_epoch_ms AS history_last_played_at_epoch_ms,
               playback_history.completed AS history_completed,
               playback_history.updated_at_epoch_ms AS history_updated_at_epoch_ms
        FROM tracks
        INNER JOIN playback_history ON playback_history.track_id = tracks.id
        WHERE playback_history.owner_user_id = :ownerUserId
        ORDER BY playback_history.last_played_at_epoch_ms DESC, playback_history.track_id DESC
        """,
    )
    fun pagedHistory(ownerUserId: String): PagingSource<Int, PlaybackHistoryReadModel>
}
