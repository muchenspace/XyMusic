package com.xymusic.app.feature.search.presentation

import androidx.lifecycle.SavedStateHandle
import androidx.paging.PagingData
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.model.media.Album
import com.xymusic.app.core.model.media.Artist
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.feature.search.domain.SearchRepository
import com.xymusic.app.feature.search.domain.SearchResult
import com.xymusic.app.feature.search.domain.SearchUseCases
import com.xymusic.app.feature.search.domain.model.SearchHistoryItem
import com.xymusic.app.feature.search.domain.model.SearchOverview
import com.xymusic.app.feature.search.domain.model.SearchQuery
import com.xymusic.app.feature.search.domain.model.SearchScope
import com.xymusic.app.support.MainDispatcherRule
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Rule
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class SearchViewModelTest {
    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    @Test
    fun automaticSearchDebouncesNormalizesAndDoesNotRecordHistory() = runTest {
        val repository = FakeSearchRepository()
        val viewModel = SearchViewModel(SearchUseCases(repository), SavedStateHandle())

        viewModel.onQueryChanged("  First   Track  ")
        advanceTimeBy(349)
        assertThat(repository.refreshOverviewCalls).isEqualTo(0)

        advanceTimeBy(1)
        advanceUntilIdle()

        assertThat(viewModel.uiState.value.activeQuery).isEqualTo("First Track")
        assertThat(repository.refreshOverviewCalls).isEqualTo(1)
        assertThat(repository.recordCalls).isEqualTo(0)
    }

    @Test
    fun explicitSubmitBypassesDebounceAndRecordsOnlyOnce() = runTest {
        val repository = FakeSearchRepository()
        val viewModel = SearchViewModel(SearchUseCases(repository), SavedStateHandle())

        viewModel.onQueryChanged("First Track")
        viewModel.submit()
        advanceUntilIdle()

        assertThat(repository.refreshOverviewCalls).isEqualTo(1)
        assertThat(repository.recordCalls).isEqualTo(1)
        assertThat(repository.lastRecordedQuery?.value).isEqualTo("First Track")
        assertThat(repository.lastRecordedScope).isEqualTo(SearchScope.ALL)
    }

    @Test
    fun overlyLongInputIsRejectedBeforeSearch() = runTest {
        val repository = FakeSearchRepository()
        val viewModel = SearchViewModel(SearchUseCases(repository), SavedStateHandle())

        viewModel.onQueryChanged("x".repeat(201))
        viewModel.submit()
        advanceUntilIdle()

        assertThat(viewModel.uiState.value.queryError).isEqualTo(SearchQueryErrorUi.TooLong)
        assertThat(repository.refreshOverviewCalls).isEqualTo(0)
    }
}

private class FakeSearchRepository : SearchRepository {
    private val history = MutableStateFlow<List<SearchHistoryItem>>(emptyList())
    var refreshOverviewCalls = 0
    var recordCalls = 0
    var lastRecordedQuery: SearchQuery? = null
    var lastRecordedScope: SearchScope? = null

    override fun observeOverview(query: SearchQuery): Flow<SearchOverview?> = flowOf(
        SearchOverview(query, emptyList(), emptyList(), emptyList()),
    )

    override suspend fun refreshOverview(query: SearchQuery): SearchResult<Unit> {
        refreshOverviewCalls += 1
        return SearchResult.Success(Unit)
    }

    override fun pagedTracks(query: SearchQuery): Flow<PagingData<Track>> = flowOf(PagingData.empty())

    override fun pagedArtists(query: SearchQuery): Flow<PagingData<Artist>> = flowOf(PagingData.empty())

    override fun pagedAlbums(query: SearchQuery): Flow<PagingData<Album>> = flowOf(PagingData.empty())

    override fun observeHistory(): Flow<List<SearchHistoryItem>> = history

    override suspend fun record(query: SearchQuery, scope: SearchScope): SearchResult<Unit> {
        recordCalls += 1
        lastRecordedQuery = query
        lastRecordedScope = scope
        return SearchResult.Success(Unit)
    }

    override suspend fun delete(query: SearchQuery, scope: SearchScope): SearchResult<Unit> = SearchResult.Success(Unit)

    override suspend fun clearHistory(): SearchResult<Unit> = SearchResult.Success(Unit)
}
