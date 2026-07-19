package com.xymusic.app.core.database

import android.app.Application
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.database.entity.CatalogRemoteKeyEntity
import com.xymusic.app.core.database.model.CatalogItemType
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class CatalogRemoteKeyDaoTest {
    private lateinit var database: XyMusicDatabase

    @Before
    fun setUp() {
        database =
            Room
                .inMemoryDatabaseBuilder(
                    ApplicationProvider.getApplicationContext(),
                    XyMusicDatabase::class.java,
                ).allowMainThreadQueries()
                .build()
    }

    @After
    fun tearDown() {
        database.close()
    }

    @Test
    fun replaceAndAppendMaintainContinuousPositionsAndCursorChain() = runTest {
        val dao = database.catalogRemoteKeyDao()
        dao.replace(
            collectionKey = COLLECTION_KEY,
            itemType = CatalogItemType.TRACK,
            keys =
            listOf(
                key("track-1", position = 0, previousCursor = null, nextCursor = "cursor-2"),
                key("track-2", position = 1, previousCursor = null, nextCursor = "cursor-2"),
            ),
        )
        dao.append(
            collectionKey = COLLECTION_KEY,
            itemType = CatalogItemType.TRACK,
            keys =
            listOf(
                key("track-3", position = 2, previousCursor = "cursor-2", nextCursor = null),
                key("track-4", position = 3, previousCursor = "cursor-2", nextCursor = null),
            ),
        )

        assertThat(dao.keys(COLLECTION_KEY, CatalogItemType.TRACK).map { it.itemId })
            .containsExactly("track-1", "track-2", "track-3", "track-4")
            .inOrder()
        assertThat(dao.lastKey(COLLECTION_KEY, CatalogItemType.TRACK)?.position).isEqualTo(3L)
        assertThat(dao.lastNextCursor(COLLECTION_KEY, CatalogItemType.TRACK)).isNull()
    }

    @Test
    fun invalidReplacementFailsBeforeClearingExistingSnapshot() = runTest {
        val dao = database.catalogRemoteKeyDao()
        dao.replace(
            collectionKey = COLLECTION_KEY,
            itemType = CatalogItemType.TRACK,
            keys = listOf(key("track-existing", position = 0, nextCursor = null)),
        )

        val failure =
            runCatching {
                dao.replace(
                    collectionKey = COLLECTION_KEY,
                    itemType = CatalogItemType.TRACK,
                    keys =
                    listOf(
                        key("track-new-1", position = 0, nextCursor = null),
                        key("track-new-2", position = 2, nextCursor = null),
                    ),
                )
            }.exceptionOrNull()

        assertThat(failure).isInstanceOf(IllegalArgumentException::class.java)
        assertThat(dao.keys(COLLECTION_KEY, CatalogItemType.TRACK).map { it.itemId })
            .containsExactly("track-existing")
    }

    @Test
    fun appendRejectsDuplicateItemFromEarlierPageWithoutChangingSnapshot() = runTest {
        val dao = database.catalogRemoteKeyDao()
        dao.replace(
            collectionKey = COLLECTION_KEY,
            itemType = CatalogItemType.TRACK,
            keys = listOf(key("track-1", position = 0, nextCursor = "cursor-1")),
        )

        val failure =
            runCatching {
                dao.append(
                    collectionKey = COLLECTION_KEY,
                    itemType = CatalogItemType.TRACK,
                    keys =
                    listOf(
                        key(
                            itemId = "track-1",
                            position = 1,
                            previousCursor = "cursor-1",
                            nextCursor = null,
                        ),
                    ),
                )
            }.exceptionOrNull()

        assertThat(failure).isInstanceOf(IllegalArgumentException::class.java)
        assertThat(dao.keys(COLLECTION_KEY, CatalogItemType.TRACK).map { it.itemId })
            .containsExactly("track-1")
    }

    @Test
    fun appendRejectsWrongCursorPositionAndPageDuplicates() = runTest {
        val dao = database.catalogRemoteKeyDao()
        dao.replace(
            collectionKey = COLLECTION_KEY,
            itemType = CatalogItemType.TRACK,
            keys = listOf(key("track-1", position = 0, nextCursor = "cursor-1")),
        )

        val wrongCursor =
            runCatching {
                dao.append(
                    collectionKey = COLLECTION_KEY,
                    itemType = CatalogItemType.TRACK,
                    keys =
                    listOf(
                        key("track-2", position = 1, previousCursor = "wrong", nextCursor = null),
                    ),
                )
            }.exceptionOrNull()
        val positionGap =
            runCatching {
                dao.append(
                    collectionKey = COLLECTION_KEY,
                    itemType = CatalogItemType.TRACK,
                    keys =
                    listOf(
                        key("track-2", position = 2, previousCursor = "cursor-1", nextCursor = null),
                    ),
                )
            }.exceptionOrNull()
        val duplicateInPage =
            runCatching {
                dao.append(
                    collectionKey = COLLECTION_KEY,
                    itemType = CatalogItemType.TRACK,
                    keys =
                    listOf(
                        key("track-2", position = 1, previousCursor = "cursor-1", nextCursor = null),
                        key("track-2", position = 2, previousCursor = "cursor-1", nextCursor = null),
                    ),
                )
            }.exceptionOrNull()

        assertThat(wrongCursor).isInstanceOf(IllegalArgumentException::class.java)
        assertThat(positionGap).isInstanceOf(IllegalArgumentException::class.java)
        assertThat(duplicateInPage).isInstanceOf(IllegalArgumentException::class.java)
        assertThat(dao.keys(COLLECTION_KEY, CatalogItemType.TRACK).map { it.itemId })
            .containsExactly("track-1")
    }

    @Test
    fun appendRequiresExistingNonTerminalCollection() = runTest {
        val dao = database.catalogRemoteKeyDao()
        val missingCollection =
            runCatching {
                dao.append(
                    collectionKey = COLLECTION_KEY,
                    itemType = CatalogItemType.TRACK,
                    keys = listOf(key("track-1", position = 0, nextCursor = null)),
                )
            }.exceptionOrNull()

        dao.replace(
            collectionKey = COLLECTION_KEY,
            itemType = CatalogItemType.TRACK,
            keys = listOf(key("track-1", position = 0, nextCursor = null)),
        )
        val terminalCollection =
            runCatching {
                dao.append(
                    collectionKey = COLLECTION_KEY,
                    itemType = CatalogItemType.TRACK,
                    keys =
                    listOf(
                        key("track-2", position = 1, previousCursor = null, nextCursor = null),
                    ),
                )
            }.exceptionOrNull()

        assertThat(missingCollection).isInstanceOf(IllegalArgumentException::class.java)
        assertThat(terminalCollection).isInstanceOf(IllegalArgumentException::class.java)
    }

    @Test
    fun markEndOfPaginationClearsTheCurrentLastPageCursorAndRejectsStaleBoundary() = runTest {
        val dao = database.catalogRemoteKeyDao()
        dao.replace(
            collectionKey = COLLECTION_KEY,
            itemType = CatalogItemType.TRACK,
            keys =
            listOf(
                key("track-1", position = 0, nextCursor = "cursor-2"),
                key("track-2", position = 1, nextCursor = "cursor-2"),
            ),
        )

        val staleBoundary =
            runCatching {
                dao.markEndOfPagination(
                    collectionKey = COLLECTION_KEY,
                    itemType = CatalogItemType.TRACK,
                    expectedCursor = "cursor-2",
                    expectedLastPosition = 0,
                    refreshedAtEpochMs = 2_000,
                )
            }.exceptionOrNull()
        assertThat(staleBoundary).isInstanceOf(IllegalStateException::class.java)

        dao.markEndOfPagination(
            collectionKey = COLLECTION_KEY,
            itemType = CatalogItemType.TRACK,
            expectedCursor = "cursor-2",
            expectedLastPosition = 1,
            refreshedAtEpochMs = 2_000,
        )

        val keys = dao.keys(COLLECTION_KEY, CatalogItemType.TRACK)
        assertThat(keys.all { it.nextCursor == null }).isTrue()
        assertThat(keys.map { it.refreshedAtEpochMs }).containsExactly(2_000L, 2_000L)
        assertThat(dao.lastNextCursor(COLLECTION_KEY, CatalogItemType.TRACK)).isNull()
    }

    @Test
    fun searchCollectionUsageAndExpiryOperateOnWholeCollectionsOnly() = runTest {
        val dao = database.catalogRemoteKeyDao()
        val expiredCollection = "search:v1:expired"
        val usedCollection = "search:v1:used"
        val freshCollection = "search:v1:fresh"
        dao.replace(
            expiredCollection,
            CatalogItemType.TRACK,
            listOf(
                key(
                    itemId = "expired-track",
                    position = 0,
                    nextCursor = null,
                    collectionKey = expiredCollection,
                    refreshedAtEpochMs = 100,
                ),
            ),
        )
        dao.replace(
            usedCollection,
            CatalogItemType.TRACK,
            listOf(
                key(
                    itemId = "used-track",
                    position = 0,
                    nextCursor = null,
                    collectionKey = usedCollection,
                    refreshedAtEpochMs = 100,
                ),
            ),
        )
        dao.replace(
            usedCollection,
            CatalogItemType.ARTIST,
            listOf(
                key(
                    itemId = "used-artist",
                    position = 0,
                    nextCursor = null,
                    collectionKey = usedCollection,
                    itemType = CatalogItemType.ARTIST,
                    refreshedAtEpochMs = 100,
                ),
            ),
        )
        dao.replace(
            freshCollection,
            CatalogItemType.ALBUM,
            listOf(
                key(
                    itemId = "fresh-album",
                    position = 0,
                    nextCursor = null,
                    collectionKey = freshCollection,
                    itemType = CatalogItemType.ALBUM,
                    refreshedAtEpochMs = 400,
                ),
            ),
        )
        dao.replace(
            COLLECTION_KEY,
            CatalogItemType.TRACK,
            listOf(key("non-search-track", position = 0, nextCursor = null)),
        )

        assertThat(dao.markSearchCollectionUsed(usedCollection, usedAtEpochMs = 500)).isEqualTo(2)
        assertThat(dao.searchCollectionLastUsedAt(usedCollection)).isEqualTo(500L)
        assertThat(dao.pruneSearchCollections(expireBeforeEpochMs = 300)).isEqualTo(1)

        assertThat(dao.keys(expiredCollection, CatalogItemType.TRACK)).isEmpty()
        assertThat(dao.keys(usedCollection, CatalogItemType.TRACK)).isNotEmpty()
        assertThat(dao.keys(usedCollection, CatalogItemType.ARTIST)).isNotEmpty()
        assertThat(dao.keys(freshCollection, CatalogItemType.ALBUM)).isNotEmpty()
        assertThat(dao.keys(COLLECTION_KEY, CatalogItemType.TRACK)).isNotEmpty()
    }

    @Test
    fun searchCollectionPruningKeepsOnlyFiftyMostRecentlyUsedCollections() = runTest {
        val dao = database.catalogRemoteKeyDao()
        repeat(52) { index ->
            val collectionKey = "search:v1:query-$index"
            dao.replace(
                collectionKey,
                CatalogItemType.TRACK,
                listOf(
                    key(
                        itemId = "track-$index",
                        position = 0,
                        nextCursor = null,
                        collectionKey = collectionKey,
                        refreshedAtEpochMs = index.toLong(),
                    ),
                ),
            )
        }

        assertThat(
            dao.pruneSearchCollections(
                expireBeforeEpochMs = 0,
                maxCollections = 50,
            ),
        ).isEqualTo(2)

        assertThat(dao.keys("search:v1:query-0", CatalogItemType.TRACK)).isEmpty()
        assertThat(dao.keys("search:v1:query-1", CatalogItemType.TRACK)).isEmpty()
        assertThat(dao.keys("search:v1:query-2", CatalogItemType.TRACK)).isNotEmpty()
        assertThat(dao.keys("search:v1:query-51", CatalogItemType.TRACK)).isNotEmpty()
    }

    private fun key(
        itemId: String,
        position: Long,
        previousCursor: String? = null,
        nextCursor: String?,
        collectionKey: String = COLLECTION_KEY,
        itemType: CatalogItemType = CatalogItemType.TRACK,
        refreshedAtEpochMs: Long = 1_000,
    ) = CatalogRemoteKeyEntity(
        collectionKey = collectionKey,
        itemType = itemType,
        itemId = itemId,
        position = position,
        previousCursor = previousCursor,
        nextCursor = nextCursor,
        refreshedAtEpochMs = refreshedAtEpochMs,
    )

    private companion object {
        const val COLLECTION_KEY = "catalog:test"
    }
}
