package com.xymusic.app.core.database.dao

import androidx.room.Dao
import androidx.room.Query
import androidx.room.Transaction
import androidx.room.Upsert
import com.xymusic.app.core.database.entity.SearchHistoryEntity
import com.xymusic.app.core.database.model.SearchScope
import kotlinx.coroutines.flow.Flow

@Dao
abstract class SearchHistoryDao {
    @Upsert
    protected abstract suspend fun upsert(entry: SearchHistoryEntity)

    @Query(
        """
        DELETE FROM search_history
        WHERE owner_user_id = :ownerUserId
          AND rowid NOT IN (
              SELECT rowid FROM search_history
              WHERE owner_user_id = :ownerUserId
              ORDER BY searched_at_epoch_ms DESC, normalized_query, scope
              LIMIT :limit
          )
        """,
    )
    protected abstract suspend fun trim(ownerUserId: String, limit: Int)

    @Transaction
    open suspend fun record(entry: SearchHistoryEntity, limit: Int = DEFAULT_LIMIT) {
        require(limit > 0) { "Search history limit must be positive" }
        upsert(entry)
        trim(entry.ownerUserId, limit)
    }

    @Query(
        """
        SELECT * FROM search_history
        WHERE owner_user_id = :ownerUserId
        ORDER BY searched_at_epoch_ms DESC, normalized_query, scope
        LIMIT :limit
        """,
    )
    abstract fun observe(ownerUserId: String, limit: Int = DEFAULT_LIMIT): Flow<List<SearchHistoryEntity>>

    @Query(
        """
        DELETE FROM search_history
        WHERE owner_user_id = :ownerUserId
            AND normalized_query = :normalizedQuery
            AND scope = :scope
        """,
    )
    abstract suspend fun delete(ownerUserId: String, normalizedQuery: String, scope: SearchScope): Int

    @Query("DELETE FROM search_history WHERE owner_user_id = :ownerUserId")
    abstract suspend fun clear(ownerUserId: String): Int

    companion object {
        const val DEFAULT_LIMIT = 50
    }
}
