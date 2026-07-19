package com.xymusic.app.core.database.dao

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import androidx.room.Transaction
import com.xymusic.app.core.database.entity.CatalogRemoteKeyEntity
import com.xymusic.app.core.database.model.CatalogItemType

@Dao
abstract class CatalogRemoteKeyDao {
    @Insert(onConflict = OnConflictStrategy.ABORT)
    protected abstract suspend fun insertAll(keys: List<CatalogRemoteKeyEntity>)

    @Query("DELETE FROM catalog_remote_keys WHERE collection_key = :collectionKey AND item_type = :itemType")
    abstract suspend fun clear(collectionKey: String, itemType: CatalogItemType): Int

    @Transaction
    open suspend fun replace(collectionKey: String, itemType: CatalogItemType, keys: List<CatalogRemoteKeyEntity>) {
        validatePage(
            collectionKey = collectionKey,
            itemType = itemType,
            keys = keys,
            expectedFirstPosition = FIRST_POSITION,
            expectedPreviousCursor = null,
        )
        clear(collectionKey, itemType)
        if (keys.isNotEmpty()) insertAll(keys)
    }

    @Transaction
    open suspend fun append(collectionKey: String, itemType: CatalogItemType, keys: List<CatalogRemoteKeyEntity>) {
        if (keys.isEmpty()) return

        val previousLastKey =
            requireNotNull(lastKey(collectionKey, itemType)) {
                "Cannot append remote keys before the collection is refreshed"
            }
        val previousCursor =
            requireNotNull(previousLastKey.nextCursor) {
                "Cannot append remote keys after the collection reached its end"
            }
        validatePage(
            collectionKey = collectionKey,
            itemType = itemType,
            keys = keys,
            expectedFirstPosition = previousLastKey.position + 1,
            expectedPreviousCursor = previousCursor,
        )

        val duplicateIds =
            existingItemIds(
                collectionKey = collectionKey,
                itemType = itemType,
                itemIds = keys.map(CatalogRemoteKeyEntity::itemId),
            )
        require(duplicateIds.isEmpty()) {
            "Remote key item IDs must not repeat across pages: ${duplicateIds.sorted()}"
        }
        insertAll(keys)
    }

    @Query(
        """
        SELECT * FROM catalog_remote_keys
        WHERE collection_key = :collectionKey AND item_type = :itemType AND item_id = :itemId
        """,
    )
    abstract suspend fun key(collectionKey: String, itemType: CatalogItemType, itemId: String): CatalogRemoteKeyEntity?

    @Query(
        """
        SELECT * FROM catalog_remote_keys
        WHERE collection_key = :collectionKey AND item_type = :itemType
        ORDER BY position DESC
        LIMIT 1
        """,
    )
    abstract suspend fun lastKey(collectionKey: String, itemType: CatalogItemType): CatalogRemoteKeyEntity?

    @Query(
        """
        SELECT next_cursor FROM catalog_remote_keys
        WHERE collection_key = :collectionKey AND item_type = :itemType
        ORDER BY position DESC
        LIMIT 1
        """,
    )
    abstract suspend fun lastNextCursor(collectionKey: String, itemType: CatalogItemType): String?

    @Query(
        """
        UPDATE catalog_remote_keys
        SET next_cursor = NULL,
            refreshed_at_epoch_ms = :refreshedAtEpochMs
        WHERE collection_key = :collectionKey
            AND item_type = :itemType
            AND next_cursor = :expectedCursor
            AND (
                (:previousCursor IS NULL AND previous_cursor IS NULL)
                OR previous_cursor = :previousCursor
            )
        """,
    )
    protected abstract suspend fun clearNextCursorForPage(
        collectionKey: String,
        itemType: CatalogItemType,
        expectedCursor: String,
        previousCursor: String?,
        refreshedAtEpochMs: Long,
    ): Int

    @Transaction
    open suspend fun markEndOfPagination(
        collectionKey: String,
        itemType: CatalogItemType,
        expectedCursor: String,
        expectedLastPosition: Long,
        refreshedAtEpochMs: Long,
    ) {
        require(expectedCursor.isNotBlank()) { "Expected pagination cursor must not be blank" }
        val currentLastKey =
            requireNotNull(lastKey(collectionKey, itemType)) {
                "Remote key collection does not have a pagination boundary"
            }
        check(
            currentLastKey.position == expectedLastPosition &&
                currentLastKey.nextCursor == expectedCursor,
        ) {
            "Remote key pagination boundary changed before it could be marked complete"
        }
        check(
            clearNextCursorForPage(
                collectionKey = collectionKey,
                itemType = itemType,
                expectedCursor = expectedCursor,
                previousCursor = currentLastKey.previousCursor,
                refreshedAtEpochMs = refreshedAtEpochMs,
            ) > 0,
        ) {
            "Remote key terminal page could not be marked complete"
        }
    }

    @Query(
        """
        SELECT item_id FROM catalog_remote_keys
        WHERE collection_key = :collectionKey
            AND item_type = :itemType
            AND item_id IN (:itemIds)
        """,
    )
    abstract suspend fun existingItemIds(
        collectionKey: String,
        itemType: CatalogItemType,
        itemIds: List<String>,
    ): List<String>

    @Query(
        """
        SELECT * FROM catalog_remote_keys
        WHERE collection_key = :collectionKey AND item_type = :itemType
        ORDER BY position ASC
        """,
    )
    abstract suspend fun keys(collectionKey: String, itemType: CatalogItemType): List<CatalogRemoteKeyEntity>

    @Query(
        """
        UPDATE catalog_remote_keys
        SET refreshed_at_epoch_ms = :usedAtEpochMs
        WHERE collection_key = :collectionKey
            AND collection_key LIKE 'search:v1:%'
        """,
    )
    protected abstract suspend fun touchSearchCollection(collectionKey: String, usedAtEpochMs: Long): Int

    @Transaction
    open suspend fun markSearchCollectionUsed(collectionKey: String, usedAtEpochMs: Long): Int {
        requireSearchCollectionKey(collectionKey)
        require(usedAtEpochMs >= 0) { "Search collection use time must not be negative" }
        return touchSearchCollection(collectionKey, usedAtEpochMs)
    }

    @Query(
        """
        SELECT MAX(refreshed_at_epoch_ms) FROM catalog_remote_keys
        WHERE collection_key = :collectionKey
            AND collection_key LIKE 'search:v1:%'
        """,
    )
    abstract suspend fun searchCollectionLastUsedAt(collectionKey: String): Long?

    @Query(
        """
        DELETE FROM catalog_remote_keys
        WHERE collection_key IN (
            SELECT collection_key FROM catalog_remote_keys
            WHERE collection_key LIKE 'search:v1:%'
            GROUP BY collection_key
            HAVING MAX(refreshed_at_epoch_ms) < :expireBeforeEpochMs
        )
        """,
    )
    protected abstract suspend fun deleteExpiredSearchCollections(expireBeforeEpochMs: Long): Int

    @Query(
        """
        DELETE FROM catalog_remote_keys
        WHERE collection_key LIKE 'search:v1:%'
            AND collection_key NOT IN (
                SELECT collection_key FROM catalog_remote_keys
                WHERE collection_key LIKE 'search:v1:%'
                GROUP BY collection_key
                ORDER BY MAX(refreshed_at_epoch_ms) DESC, collection_key ASC
                LIMIT :maxCollections
            )
        """,
    )
    protected abstract suspend fun trimSearchCollections(maxCollections: Int): Int

    @Transaction
    open suspend fun pruneSearchCollections(
        expireBeforeEpochMs: Long,
        maxCollections: Int = MAX_SEARCH_COLLECTIONS,
    ): Int {
        require(expireBeforeEpochMs >= 0) { "Search collection expiry time must not be negative" }
        require(maxCollections in 1..MAX_SEARCH_COLLECTIONS) {
            "Search collection limit must be between 1 and $MAX_SEARCH_COLLECTIONS"
        }
        val expiredKeyCount = deleteExpiredSearchCollections(expireBeforeEpochMs)
        val overflowKeyCount = trimSearchCollections(maxCollections)
        return expiredKeyCount + overflowKeyCount
    }

    private fun validatePage(
        collectionKey: String,
        itemType: CatalogItemType,
        keys: List<CatalogRemoteKeyEntity>,
        expectedFirstPosition: Long,
        expectedPreviousCursor: String?,
    ) {
        if (keys.isEmpty()) return
        require(keys.all { it.collectionKey == collectionKey && it.itemType == itemType }) {
            "Remote key belongs to a different collection or item type"
        }
        require(keys.map { it.itemId }.distinct().size == keys.size) {
            "Remote key item IDs must be unique within a page"
        }
        require(keys.map { it.previousCursor }.distinct().size == 1) {
            "Remote keys in one page must share the same previous cursor"
        }
        require(keys.map { it.nextCursor }.distinct().size == 1) {
            "Remote keys in one page must share the same next cursor"
        }
        require(keys.first().previousCursor == expectedPreviousCursor) {
            "Remote key page does not continue from the expected cursor"
        }

        keys.sortedBy(CatalogRemoteKeyEntity::position).forEachIndexed { index, key ->
            require(key.position == expectedFirstPosition + index) {
                "Remote key positions must be contiguous from $expectedFirstPosition"
            }
        }
    }

    private companion object {
        const val FIRST_POSITION = 0L
        const val SEARCH_COLLECTION_PREFIX = "search:v1:"
        const val MAX_SEARCH_COLLECTIONS = 50
    }

    private fun requireSearchCollectionKey(collectionKey: String) {
        require(collectionKey.startsWith(SEARCH_COLLECTION_PREFIX)) {
            "Search collection key must start with $SEARCH_COLLECTION_PREFIX"
        }
    }
}
