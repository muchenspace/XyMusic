package com.xymusic.app.core.database

import android.app.Application
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.database.entity.SearchHistoryEntity
import com.xymusic.app.core.database.model.SearchScope
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class SearchHistoryDaoTest {
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
    fun singleDeleteIsScopedByOwnerNormalizedQueryAndScope() = runTest {
        val dao = database.searchHistoryDao()
        dao.record(history("alice", SearchScope.ALL, searchedAtEpochMs = 3_000))
        dao.record(history("alice", SearchScope.TRACKS, searchedAtEpochMs = 2_000))
        dao.record(history("bob", SearchScope.ALL, searchedAtEpochMs = 1_000))

        assertThat(
            dao.delete(
                ownerUserId = "alice",
                normalizedQuery = NORMALIZED_QUERY,
                scope = SearchScope.ALL,
            ),
        ).isEqualTo(1)

        assertThat(dao.observe("alice").first().map { it.scope })
            .containsExactly(SearchScope.TRACKS)
        assertThat(dao.observe("bob").first().map { it.scope })
            .containsExactly(SearchScope.ALL)
        assertThat(
            dao.delete(
                ownerUserId = "alice",
                normalizedQuery = "different-query",
                scope = SearchScope.TRACKS,
            ),
        ).isEqualTo(0)
    }

    private fun history(ownerUserId: String, scope: SearchScope, searchedAtEpochMs: Long) = SearchHistoryEntity(
        ownerUserId = ownerUserId,
        normalizedQuery = NORMALIZED_QUERY,
        scope = scope,
        query = "Search Query",
        searchedAtEpochMs = searchedAtEpochMs,
    )

    private companion object {
        const val NORMALIZED_QUERY = "search query"
    }
}
