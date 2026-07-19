package com.xymusic.app.feature.search.presentation

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.paging.PagingData
import androidx.paging.cachedIn
import androidx.paging.map
import com.xymusic.app.core.model.media.Album
import com.xymusic.app.core.model.media.Artist
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.core.ui.media.toUi
import com.xymusic.app.feature.search.domain.SearchResult
import com.xymusic.app.feature.search.domain.SearchUseCases
import com.xymusic.app.feature.search.domain.model.SearchHistoryItem
import com.xymusic.app.feature.search.domain.model.SearchOverview
import com.xymusic.app.feature.search.domain.model.SearchQuery
import com.xymusic.app.feature.search.domain.model.SearchScope
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.mapNotNull
import kotlinx.coroutines.flow.merge
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

@OptIn(FlowPreview::class, ExperimentalCoroutinesApi::class)
@HiltViewModel
class SearchViewModel
@Inject
constructor(
    private val useCases: SearchUseCases,
    private val savedStateHandle: SavedStateHandle,
) : ViewModel() {
    private val input = savedStateHandle.getStateFlow(KEY_INPUT, "")
    private val scopeName = savedStateHandle.getStateFlow(KEY_SCOPE, SearchScope.ALL.name)
    private val activeSearch = MutableStateFlow<ActiveSearch?>(null)
    private val explicitRequests = Channel<RequestEvent>(capacity = Channel.BUFFERED)
    private val mutableUiState =
        MutableStateFlow(
            SearchUiState(
                input = input.value,
                selectedScope = scopeName.value.toScope(),
            ),
        )
    val uiState = mutableUiState.asStateFlow()

    private val mutableEffects = MutableSharedFlow<SearchUiEffect>(extraBufferCapacity = 1)
    val effects: SharedFlow<SearchUiEffect> = mutableEffects.asSharedFlow()

    val tracks =
        activeSearch
            .flatMapLatest { active ->
                if (active?.request?.scope == SearchScope.TRACKS) {
                    useCases.tracks(active.request.query)
                } else {
                    flowOf(PagingData.empty<Track>())
                }
            }.map { pagingData -> pagingData.map { it.toUi() } }
            .cachedIn(viewModelScope)

    val artists =
        activeSearch
            .flatMapLatest { active ->
                if (active?.request?.scope == SearchScope.ARTISTS) {
                    useCases.artists(active.request.query)
                } else {
                    flowOf(PagingData.empty<Artist>())
                }
            }.map { pagingData -> pagingData.map { it.toUi() } }
            .cachedIn(viewModelScope)

    val albums =
        activeSearch
            .flatMapLatest { active ->
                if (active?.request?.scope == SearchScope.ALBUMS) {
                    useCases.albums(active.request.query)
                } else {
                    flowOf(PagingData.empty<Album>())
                }
            }.map { pagingData -> pagingData.map { it.toUi() } }
            .cachedIn(viewModelScope)

    init {
        observeRequests()
        observeOverview()
        observeHistory()
    }

    fun onQueryChanged(value: String) {
        savedStateHandle[KEY_INPUT] = value
        mutableUiState.update { state ->
            state.copy(
                input = value,
                queryError =
                if (normalizedLength(value) > MAX_QUERY_LENGTH) {
                    SearchQueryErrorUi.TooLong
                } else {
                    null
                },
            )
        }
        if (value.isBlank() || normalizedLength(value) > MAX_QUERY_LENGTH) clearActiveSearch()
    }

    fun onScopeSelected(scope: SearchScope) {
        savedStateHandle[KEY_SCOPE] = scope.name
        mutableUiState.update { it.copy(selectedScope = scope) }
        queryOrNull(mutableUiState.value.input, reportError = false)?.let { query ->
            explicitRequests.trySend(
                RequestEvent(
                    request = SearchRequest(query, scope),
                    explicit = true,
                    recordHistory = false,
                ),
            )
        }
    }

    fun submit() {
        val query = queryOrNull(mutableUiState.value.input, reportError = true) ?: return
        savedStateHandle[KEY_INPUT] = query.value
        mutableUiState.update { it.copy(input = query.value, queryError = null) }
        explicitRequests.trySend(
            RequestEvent(
                request = SearchRequest(query, scopeName.value.toScope()),
                explicit = true,
                recordHistory = true,
            ),
        )
    }

    fun clearQuery() {
        savedStateHandle[KEY_INPUT] = ""
        mutableUiState.update {
            it.copy(
                input = "",
                activeQuery = null,
                overview = null,
                isOverviewRefreshing = false,
                overviewRefreshFailed = false,
                queryError = null,
            )
        }
        activeSearch.value = null
    }

    fun selectHistory(item: SearchHistoryUi) {
        val query = queryOrNull(item.query, reportError = false) ?: return
        savedStateHandle[KEY_INPUT] = query.value
        savedStateHandle[KEY_SCOPE] = item.scope.name
        mutableUiState.update {
            it.copy(
                input = query.value,
                selectedScope = item.scope,
                queryError = null,
            )
        }
        explicitRequests.trySend(
            RequestEvent(
                request = SearchRequest(query, item.scope),
                explicit = true,
                recordHistory = true,
            ),
        )
    }

    fun recordOpenedResult() {
        val request = activeSearch.value?.request ?: return
        viewModelScope.launch {
            recordHistory(request)
        }
    }

    fun deleteHistory(item: SearchHistoryUi) {
        val query = queryOrNull(item.query, reportError = false) ?: return
        viewModelScope.launch {
            if (executeSafely { useCases.delete(query, item.scope) } !is SearchResult.Success) {
                mutableEffects.emit(SearchUiEffect.HistoryUpdateFailed)
            }
        }
    }

    fun clearHistory() {
        viewModelScope.launch {
            if (executeSafely { useCases.clearHistory() } !is SearchResult.Success) {
                mutableEffects.emit(SearchUiEffect.HistoryUpdateFailed)
            }
        }
    }

    private fun observeRequests() {
        val automaticRequests =
            input
                .debounce(SEARCH_DEBOUNCE_MILLIS)
                .mapNotNull { queryOrNull(it, reportError = false) }
                .distinctUntilChanged()
                .map { query -> SearchRequest(query, scopeName.value.toScope()) }
                .map {
                    RequestEvent(
                        request = it,
                        explicit = false,
                        recordHistory = false,
                    )
                }

        viewModelScope.launch {
            merge(
                automaticRequests,
                explicitRequests.receiveAsFlow(),
            ).collectLatest { event ->
                if (!event.explicit && activeSearch.value?.request == event.request) {
                    return@collectLatest
                }
                activate(event.request, event.recordHistory)
            }
        }
    }

    private fun observeOverview() {
        viewModelScope.launch {
            activeSearch
                .flatMapLatest { active ->
                    if (active?.request?.scope == SearchScope.ALL) {
                        useCases.observeOverview(active.request.query)
                    } else {
                        flowOf(null)
                    }
                }.collectLatest { overview ->
                    mutableUiState.update { it.copy(overview = overview?.toUi()) }
                }
        }
    }

    private fun observeHistory() {
        viewModelScope.launch {
            useCases.observeHistory().collectLatest { history ->
                mutableUiState.update { state ->
                    state.copy(history = history.map(SearchHistoryItem::toUi))
                }
            }
        }
    }

    private suspend fun activate(request: SearchRequest, shouldRecordHistory: Boolean) {
        val generation = (activeSearch.value?.generation ?: 0L) + 1L
        activeSearch.value = ActiveSearch(request, generation)
        mutableUiState.update {
            it.copy(
                selectedScope = request.scope,
                activeQuery = request.query.value,
                overview = null,
                isOverviewRefreshing = request.scope == SearchScope.ALL,
                overviewRefreshFailed = false,
                queryError = null,
            )
        }

        if (shouldRecordHistory) recordHistory(request)

        if (request.scope == SearchScope.ALL) {
            val refreshFailed =
                executeSafely {
                    useCases.refreshOverview(request.query)
                } !is SearchResult.Success
            mutableUiState.update {
                it.copy(
                    isOverviewRefreshing = false,
                    overviewRefreshFailed = refreshFailed,
                )
            }
        }
    }

    private fun clearActiveSearch() {
        activeSearch.value = null
        mutableUiState.update {
            it.copy(
                activeQuery = null,
                overview = null,
                isOverviewRefreshing = false,
                overviewRefreshFailed = false,
            )
        }
    }

    private fun queryOrNull(rawValue: String, reportError: Boolean): SearchQuery? {
        val query = runCatching { SearchQuery.from(rawValue) }.getOrNull()
        if (reportError && query == null && normalizedLength(rawValue) > MAX_QUERY_LENGTH) {
            mutableUiState.update { it.copy(queryError = SearchQueryErrorUi.TooLong) }
        }
        return query
    }

    private suspend fun recordHistory(request: SearchRequest) {
        if (executeSafely {
                useCases.record(request.query, request.scope)
            } !is SearchResult.Success
        ) {
            mutableEffects.emit(SearchUiEffect.HistoryUpdateFailed)
        }
    }

    private suspend fun <T> executeSafely(block: suspend () -> T): T? = try {
        block()
    } catch (cancellation: CancellationException) {
        throw cancellation
    } catch (_: Exception) {
        null
    }

    private data class SearchRequest(val query: SearchQuery, val scope: SearchScope)

    private data class ActiveSearch(val request: SearchRequest, val generation: Long)

    private data class RequestEvent(val request: SearchRequest, val explicit: Boolean, val recordHistory: Boolean)

    private companion object {
        const val KEY_INPUT = "search.input"
        const val KEY_SCOPE = "search.scope"
        const val SEARCH_DEBOUNCE_MILLIS = 350L
        const val MAX_QUERY_LENGTH = 200
    }
}

private fun SearchOverview.toUi(): SearchOverviewUi = SearchOverviewUi(
    tracks = tracks.map { it.toUi() },
    artists = artists.map { it.toUi() },
    albums = albums.map { it.toUi() },
)

private fun SearchHistoryItem.toUi(): SearchHistoryUi = SearchHistoryUi(
    query = query.value,
    scope = scope,
    searchedAt =
    DateTimeFormatter
        .ofLocalizedDateTime(FormatStyle.SHORT)
        .withLocale(Locale.getDefault())
        .format(Instant.ofEpochMilli(searchedAtEpochMillis).atZone(ZoneId.systemDefault())),
)

private fun String.toScope(): SearchScope = SearchScope.entries
    .firstOrNull { it.name == this }
    ?: SearchScope.ALL

private fun normalizedLength(value: String): Int = value
    .trim()
    .replace(Regex("\\s+"), " ")
    .length
