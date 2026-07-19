package com.xymusic.app.feature.search.presentation

import androidx.compose.runtime.Immutable
import com.xymusic.app.core.ui.media.CatalogAlbumUi
import com.xymusic.app.core.ui.media.CatalogArtistUi
import com.xymusic.app.core.ui.media.CatalogTrackUi
import com.xymusic.app.feature.search.domain.model.SearchScope

@Immutable
data class SearchOverviewUi(
    val tracks: List<CatalogTrackUi>,
    val artists: List<CatalogArtistUi>,
    val albums: List<CatalogAlbumUi>,
) {
    val isEmpty: Boolean
        get() = tracks.isEmpty() && artists.isEmpty() && albums.isEmpty()
}

@Immutable
data class SearchHistoryUi(val query: String, val scope: SearchScope, val searchedAt: String)

enum class SearchQueryErrorUi {
    TooLong,
}

@Immutable
data class SearchUiState(
    val input: String = "",
    val selectedScope: SearchScope = SearchScope.ALL,
    val activeQuery: String? = null,
    val overview: SearchOverviewUi? = null,
    val isOverviewRefreshing: Boolean = false,
    val overviewRefreshFailed: Boolean = false,
    val history: List<SearchHistoryUi> = emptyList(),
    val queryError: SearchQueryErrorUi? = null,
) {
    val isIdle: Boolean
        get() = activeQuery == null
}

sealed interface SearchUiEffect {
    data object HistoryUpdateFailed : SearchUiEffect
}
