package com.xymusic.app.feature.search.presentation

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import com.xymusic.app.core.ui.component.EmptyState
import com.xymusic.app.core.ui.component.LoadingState
import com.xymusic.app.core.ui.media.CachedCatalogBanner
import com.xymusic.app.core.ui.media.CatalogAlbumShelfCard
import com.xymusic.app.core.ui.media.CatalogArtistShelfCard
import com.xymusic.app.core.ui.media.CatalogTrackRow
import com.xymusic.app.core.ui.media.CatalogTrackUi
import com.xymusic.app.feature.search.domain.model.SearchScope

@Composable
internal fun SearchOverview(
    uiState: SearchUiState,
    onRetry: () -> Unit,
    onScopeSelected: (SearchScope) -> Unit,
    onAlbumClick: (String) -> Unit,
    onArtistClick: (String) -> Unit,
    onTrackPlay: (List<CatalogTrackUi>, CatalogTrackUi) -> Unit,
    onTrackMore: (String) -> Unit,
    modifier: Modifier = Modifier,
    wideLandscape: Boolean = false,
) {
    val overview = uiState.overview
    if (overview == null && uiState.isOverviewRefreshing) {
        Box(modifier = modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            LoadingState()
        }
        return
    }
    val tracks = overview?.tracks.orEmpty()
    val artists = overview?.artists.orEmpty()
    val albums = overview?.albums.orEmpty()
    if (wideLandscape) {
        LandscapeSearchOverview(
            tracks = tracks,
            artists = artists,
            albums = albums,
            refreshFailed = uiState.overviewRefreshFailed,
            actions =
            SearchOverviewActions(
                onRetry = onRetry,
                onScopeSelected = onScopeSelected,
                onAlbumClick = onAlbumClick,
                onArtistClick = onArtistClick,
                onTrackPlay = onTrackPlay,
                onTrackMore = onTrackMore,
            ),
            modifier = modifier,
        )
        return
    }
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        contentPadding = PaddingValues(bottom = 32.dp),
    ) {
        if (uiState.overviewRefreshFailed) {
            item(key = "overview-refresh-failed") {
                CachedCatalogBanner(onRetry = onRetry)
            }
        }
        if (tracks.isNotEmpty()) {
            item(key = "overview-tracks-header") {
                SearchSectionHeader(
                    title = stringResource(R.string.search_overview_tracks),
                    onViewAll = { onScopeSelected(SearchScope.TRACKS) },
                )
            }
            items(
                count = minOf(tracks.size, 5),
                key = { index -> tracks[index].id },
                contentType = { "search-overview-track" },
            ) { index ->
                val track = tracks[index]
                CatalogTrackRow(
                    track = track,
                    onClick = { onTrackPlay(tracks, track) },
                    onPlayClick = { onTrackPlay(tracks, track) },
                    onMoreClick = { onTrackMore(track.id) },
                )
            }
        }
        if (artists.isNotEmpty()) {
            item(key = "overview-artists-header") {
                SearchSectionHeader(
                    title = stringResource(R.string.catalog_artists),
                    onViewAll = { onScopeSelected(SearchScope.ARTISTS) },
                )
            }
            item(key = "overview-artists-row") {
                LazyRow(
                    contentPadding = PaddingValues(horizontal = 20.dp),
                    horizontalArrangement = Arrangement.spacedBy(14.dp),
                ) {
                    items(artists.take(8), key = { it.id }) { artist ->
                        CatalogArtistShelfCard(
                            artist = artist,
                            onClick = { onArtistClick(artist.id) },
                        )
                    }
                }
            }
        }
        if (albums.isNotEmpty()) {
            item(key = "overview-albums-header") {
                SearchSectionHeader(
                    title = stringResource(R.string.catalog_albums),
                    onViewAll = { onScopeSelected(SearchScope.ALBUMS) },
                )
            }
            item(key = "overview-albums-row") {
                LazyRow(
                    contentPadding = PaddingValues(horizontal = 20.dp),
                    horizontalArrangement = Arrangement.spacedBy(14.dp),
                ) {
                    items(albums.take(8), key = { it.id }) { album ->
                        CatalogAlbumShelfCard(
                            album = album,
                            onClick = { onAlbumClick(album.id) },
                        )
                    }
                }
            }
        }
        if (tracks.isEmpty() && artists.isEmpty() && albums.isEmpty()) {
            item(key = "empty-results") {
                Box(
                    modifier = Modifier.fillMaxWidth().height(240.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    EmptyState(
                        title = stringResource(R.string.search_no_results_title),
                        message = stringResource(R.string.search_no_results_message),
                        icon = Icons.Outlined.Search,
                    )
                }
            }
        }
    }
}

@Composable
private fun LandscapeSearchOverview(
    tracks: List<CatalogTrackUi>,
    artists: List<com.xymusic.app.core.ui.media.CatalogArtistUi>,
    albums: List<com.xymusic.app.core.ui.media.CatalogAlbumUi>,
    refreshFailed: Boolean,
    actions: SearchOverviewActions,
    modifier: Modifier = Modifier,
) {
    if (tracks.isEmpty() && artists.isEmpty() && albums.isEmpty()) {
        Box(modifier = modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            EmptyState(
                title = stringResource(R.string.search_no_results_title),
                message = stringResource(R.string.search_no_results_message),
                icon = Icons.Outlined.Search,
            )
        }
        return
    }
    Row(
        modifier = modifier.fillMaxSize().padding(horizontal = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        LazyColumn(
            modifier =
            Modifier
                .weight(1f)
                .fillMaxHeight()
                .testTag(SearchTestTags.LandscapeOverviewTracks),
            contentPadding = PaddingValues(bottom = 16.dp),
        ) {
            if (refreshFailed) {
                item(key = "overview-refresh-failed") {
                    CachedCatalogBanner(onRetry = actions.onRetry)
                }
            }
            if (tracks.isNotEmpty()) {
                item(key = "overview-tracks-header") {
                    SearchSectionHeader(
                        title = stringResource(R.string.search_overview_tracks),
                        onViewAll = { actions.onScopeSelected(SearchScope.TRACKS) },
                        compact = true,
                    )
                }
                items(
                    count = minOf(tracks.size, 8),
                    key = { index -> tracks[index].id },
                    contentType = { "search-overview-track" },
                ) { index ->
                    val track = tracks[index]
                    CatalogTrackRow(
                        track = track,
                        onClick = { actions.onTrackPlay(tracks, track) },
                        onPlayClick = { actions.onTrackPlay(tracks, track) },
                        onMoreClick = { actions.onTrackMore(track.id) },
                    )
                }
            }
        }
        LazyColumn(
            modifier =
            Modifier
                .weight(1f)
                .fillMaxHeight()
                .testTag(SearchTestTags.LandscapeOverviewMedia),
            contentPadding = PaddingValues(bottom = 16.dp),
        ) {
            if (artists.isNotEmpty()) {
                item(key = "overview-artists-header") {
                    SearchSectionHeader(
                        title = stringResource(R.string.catalog_artists),
                        onViewAll = { actions.onScopeSelected(SearchScope.ARTISTS) },
                        compact = true,
                    )
                }
                item(key = "overview-artists-row") {
                    LazyRow(
                        contentPadding = PaddingValues(horizontal = 8.dp),
                        horizontalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        items(artists.take(8), key = { it.id }) { artist ->
                            CatalogArtistShelfCard(
                                artist = artist,
                                onClick = { actions.onArtistClick(artist.id) },
                            )
                        }
                    }
                }
            }
            if (albums.isNotEmpty()) {
                item(key = "overview-albums-header") {
                    SearchSectionHeader(
                        title = stringResource(R.string.catalog_albums),
                        onViewAll = { actions.onScopeSelected(SearchScope.ALBUMS) },
                        compact = true,
                    )
                }
                item(key = "overview-albums-row") {
                    LazyRow(
                        contentPadding = PaddingValues(horizontal = 8.dp),
                        horizontalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        items(albums.take(8), key = { it.id }) { album ->
                            CatalogAlbumShelfCard(
                                album = album,
                                onClick = { actions.onAlbumClick(album.id) },
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun SearchSectionHeader(title: String, onViewAll: () -> Unit, compact: Boolean = false) {
    Row(
        modifier =
        Modifier
            .fillMaxWidth()
            .padding(
                start = if (compact) 8.dp else 20.dp,
                end = if (compact) 4.dp else 12.dp,
                top = if (compact) 8.dp else 22.dp,
                bottom = if (compact) 4.dp else 8.dp,
            ),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = title,
            modifier = Modifier.weight(1f),
            style = if (compact) MaterialTheme.typography.titleLarge else MaterialTheme.typography.headlineSmall,
            fontWeight = FontWeight.Bold,
        )
        TextButton(onClick = onViewAll) {
            Text(
                stringResource(R.string.search_view_all),
                color = MaterialTheme.colorScheme.primary,
            )
        }
    }
}

@Immutable
private data class SearchOverviewActions(
    val onRetry: () -> Unit,
    val onScopeSelected: (SearchScope) -> Unit,
    val onAlbumClick: (String) -> Unit,
    val onArtistClick: (String) -> Unit,
    val onTrackPlay: (List<CatalogTrackUi>, CatalogTrackUi) -> Unit,
    val onTrackMore: (String) -> Unit,
)
