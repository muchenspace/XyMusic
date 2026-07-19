package com.xymusic.app.feature.search.data

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.data.media.remote.AlbumSummaryDto
import com.xymusic.app.core.data.media.remote.ArtistSummaryDto
import com.xymusic.app.core.data.media.remote.RemotePage
import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.feature.search.data.remote.SearchOverviewRemote
import com.xymusic.app.feature.search.data.remote.SearchRemoteDataSource
import com.xymusic.app.feature.search.domain.SearchResult
import com.xymusic.app.feature.search.domain.model.SearchOverview
import com.xymusic.app.feature.search.domain.model.SearchQuery
import java.io.IOException
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.runTest
import org.junit.Test

class SearchOverviewRefresherTest {
    @Test
    fun networkFailureLeavesExistingOverviewStoreUntouched() = runTest {
        val store = RecordingOverviewStore()
        val refresher =
            SearchOverviewRefresher(
                remote = FakeRemoteDataSource(overviewFailure = IOException("offline")),
                store = store,
            )

        val result = refresher.refresh(SearchQuery.from("Music"))

        assertThat(result).isInstanceOf(SearchResult.Failure::class.java)
        assertThat((result as SearchResult.Failure).error)
            .isInstanceOf(DomainError.Network::class.java)
        assertThat(store.replaceCalls).isEqualTo(0)
    }

    @Test
    fun successfulOverviewReplacesCacheExactlyOnce() = runTest {
        val overview = SearchOverviewRemote(emptyList(), emptyList(), emptyList())
        val store = RecordingOverviewStore()
        val refresher =
            SearchOverviewRefresher(
                remote = FakeRemoteDataSource(overview = overview),
                store = store,
            )

        val result = refresher.refresh(SearchQuery.from("Music"))

        assertThat(result).isEqualTo(SearchResult.Success(Unit))
        assertThat(store.replaceCalls).isEqualTo(1)
        assertThat(store.lastOverview).isSameInstanceAs(overview)
    }

    private class RecordingOverviewStore : SearchOverviewStore {
        var replaceCalls = 0
        var lastOverview: SearchOverviewRemote? = null

        override fun observe(query: SearchQuery): Flow<SearchOverview?> = flowOf(null)

        override suspend fun replace(query: SearchQuery, overview: SearchOverviewRemote) {
            replaceCalls += 1
            lastOverview = overview
        }
    }

    private class FakeRemoteDataSource(
        private val overview: SearchOverviewRemote? = null,
        private val overviewFailure: IOException? = null,
    ) : SearchRemoteDataSource {
        override suspend fun overview(query: SearchQuery): SearchOverviewRemote {
            overviewFailure?.let { throw it }
            return requireNotNull(overview)
        }

        override suspend fun tracks(cursor: String?, query: SearchQuery): RemotePage<TrackSummaryDto> =
            error("Not used")

        override suspend fun artists(cursor: String?, query: SearchQuery): RemotePage<ArtistSummaryDto> =
            error("Not used")

        override suspend fun albums(cursor: String?, query: SearchQuery): RemotePage<AlbumSummaryDto> =
            error("Not used")
    }
}
