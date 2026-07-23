package com.xymusic.app.feature.search.presentation

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.AnimatedContentTransitionScope
import androidx.compose.animation.ContentTransform
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.outlined.Album
import androidx.compose.material.icons.outlined.MusicNote
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TextField
import androidx.compose.material3.TextFieldDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.paging.PagingData
import androidx.paging.compose.collectAsLazyPagingItems
import com.xymusic.app.R
import com.xymusic.app.app.playback.CatalogPlaybackViewModel
import com.xymusic.app.core.ui.layout.isWideLandscape
import com.xymusic.app.core.ui.media.CatalogAlbumRow
import com.xymusic.app.core.ui.media.CatalogAlbumUi
import com.xymusic.app.core.ui.media.CatalogArtistRow
import com.xymusic.app.core.ui.media.CatalogArtistUi
import com.xymusic.app.core.ui.media.CatalogPagedList
import com.xymusic.app.core.ui.media.CatalogTrackRow
import com.xymusic.app.core.ui.media.CatalogTrackUi
import com.xymusic.app.feature.search.domain.model.SearchScope
import com.xymusic.app.ui.theme.XyMotion
import com.xymusic.app.ui.theme.spacing
import kotlinx.coroutines.flow.Flow

private const val SEARCH_SCOPE_SLIDE_OFFSET_DIVISOR = 24

internal object SearchTestTags {
    const val Input = "search_input"
    const val ClearHistory = "search_clear_history"
    const val LandscapeHeader = "search_landscape_header"
    const val Results = "search_results"
    const val LandscapeOverviewTracks = "search_landscape_overview_tracks"
    const val LandscapeOverviewMedia = "search_landscape_overview_media"

    fun historyItem(item: SearchHistoryUi): String = "search_history_${item.scope}_${item.query.hashCode()}"

    fun scope(scope: SearchScope): String = "search_scope_${scope.name}"
}

private enum class SearchResultMode {
    Idle,
    All,
    Tracks,
    Artists,
    Albums,
}

private fun SearchUiState.toResultMode(): SearchResultMode = if (isIdle) {
    SearchResultMode.Idle
} else {
    when (selectedScope) {
        SearchScope.ALL -> SearchResultMode.All
        SearchScope.TRACKS -> SearchResultMode.Tracks
        SearchScope.ARTISTS -> SearchResultMode.Artists
        SearchScope.ALBUMS -> SearchResultMode.Albums
    }
}

private fun AnimatedContentTransitionScope<SearchResultMode>.searchResultTransition(): ContentTransform {
    if (initialState == SearchResultMode.Idle || targetState == SearchResultMode.Idle) {
        return fadeIn(tween(XyMotion.Quick, easing = XyMotion.NavigationEasing)).togetherWith(
            fadeOut(tween(XyMotion.Quick, easing = XyMotion.NavigationEasing)),
        )
    }
    val slideDirection = if (targetState.ordinal > initialState.ordinal) 1 else -1
    return (
        slideInHorizontally(
            animationSpec = tween(XyMotion.Quick, easing = XyMotion.NavigationEasing),
            initialOffsetX = { fullWidth -> fullWidth / SEARCH_SCOPE_SLIDE_OFFSET_DIVISOR * slideDirection },
        ) + fadeIn(tween(XyMotion.Quick, easing = XyMotion.NavigationEasing))
        ).togetherWith(
        slideOutHorizontally(
            animationSpec = tween(XyMotion.Quick, easing = XyMotion.NavigationEasing),
            targetOffsetX = { fullWidth -> -(fullWidth / SEARCH_SCOPE_SLIDE_OFFSET_DIVISOR) * slideDirection },
        ) + fadeOut(tween(XyMotion.Quick, easing = XyMotion.NavigationEasing)),
    )
}

@Composable
private fun SearchHeader(
    uiState: SearchUiState,
    wideLandscape: Boolean,
    onBack: (() -> Unit)?,
    onQueryChanged: (String) -> Unit,
    onSubmit: () -> Unit,
    onClearQuery: () -> Unit,
) {
    if (wideLandscape) {
        Row(
            modifier =
            Modifier
                .fillMaxWidth()
                .padding(horizontal = 8.dp, vertical = 4.dp)
                .testTag(SearchTestTags.LandscapeHeader),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (onBack != null) {
                IconButton(onClick = onBack) {
                    Icon(
                        imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                        contentDescription = stringResource(R.string.common_back),
                        tint = MaterialTheme.colorScheme.primary,
                    )
                }
            }
            Text(
                text = stringResource(R.string.navigation_search),
                style = MaterialTheme.typography.headlineMedium,
                fontWeight = FontWeight.Bold,
                modifier = Modifier.padding(horizontal = 8.dp),
            )
            SearchQueryField(
                uiState = uiState,
                onQueryChanged = onQueryChanged,
                onSubmit = onSubmit,
                onClearQuery = onClearQuery,
                modifier = Modifier.weight(1f).widthIn(max = 720.dp),
            )
        }
    } else {
        Column(
            modifier =
            Modifier
                .fillMaxWidth()
                .padding(bottom = MaterialTheme.spacing.small),
        ) {
            Row(
                modifier =
                Modifier
                    .fillMaxWidth()
                    .padding(
                        start = if (onBack == null) 20.dp else 8.dp,
                        top = 16.dp,
                        end = 20.dp,
                        bottom = 8.dp,
                    ),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (onBack != null) {
                    IconButton(onClick = onBack) {
                        Icon(
                            imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                            contentDescription = stringResource(R.string.common_back),
                            tint = MaterialTheme.colorScheme.primary,
                        )
                    }
                    Spacer(modifier = Modifier.width(MaterialTheme.spacing.small))
                }
                Text(
                    text = stringResource(R.string.navigation_search),
                    style =
                    if (onBack == null) {
                        MaterialTheme.typography.displaySmall
                    } else {
                        MaterialTheme.typography.headlineLarge
                    },
                    fontWeight = FontWeight.Bold,
                    modifier = Modifier.weight(1f),
                )
            }
            SearchQueryField(
                uiState = uiState,
                onQueryChanged = onQueryChanged,
                onSubmit = onSubmit,
                onClearQuery = onClearQuery,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 20.dp),
            )
        }
    }
}

@Composable
private fun SearchQueryField(
    uiState: SearchUiState,
    onQueryChanged: (String) -> Unit,
    onSubmit: () -> Unit,
    onClearQuery: () -> Unit,
    modifier: Modifier = Modifier,
) {
    TextField(
        value = uiState.input,
        onValueChange = onQueryChanged,
        modifier = modifier.testTag(SearchTestTags.Input),
        placeholder = {
            Text(
                text = stringResource(R.string.search_hint),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        },
        leadingIcon = {
            Icon(
                Icons.Outlined.Search,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(22.dp),
            )
        },
        trailingIcon =
        if (uiState.input.isNotEmpty()) {
            {
                IconButton(onClick = onClearQuery) {
                    Icon(
                        imageVector = Icons.Default.Close,
                        contentDescription = stringResource(R.string.common_clear),
                        tint = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.size(20.dp),
                    )
                }
            }
        } else {
            null
        },
        isError = uiState.queryError != null,
        supportingText =
        when (uiState.queryError) {
            SearchQueryErrorUi.TooLong -> {
                { Text(stringResource(R.string.search_query_too_long)) }
            }
            null -> null
        },
        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
        keyboardActions = KeyboardActions(onSearch = { onSubmit() }),
        singleLine = true,
        shape = RoundedCornerShape(11.dp),
        colors =
        TextFieldDefaults.colors(
            focusedContainerColor = MaterialTheme.colorScheme.surfaceContainerHigh,
            unfocusedContainerColor = MaterialTheme.colorScheme.surfaceContainerHigh,
            focusedIndicatorColor = Color.Transparent,
            unfocusedIndicatorColor = Color.Transparent,
            cursorColor = MaterialTheme.colorScheme.primary,
            focusedTextColor = MaterialTheme.colorScheme.onSurface,
            unfocusedTextColor = MaterialTheme.colorScheme.onSurface,
        ),
    )
}

@Composable
fun SearchScreen(
    onTrackMore: (String) -> Unit,
    onAlbumClick: (String) -> Unit,
    onArtistClick: (String) -> Unit,
    modifier: Modifier = Modifier,
    onBack: (() -> Unit)? = null,
    viewModel: SearchViewModel = hiltViewModel(),
    playbackViewModel: CatalogPlaybackViewModel = hiltViewModel(),
) {
    val uiState by viewModel.uiState.collectAsStateWithLifecycle()
    val snackbarHostState = remember { SnackbarHostState() }
    val historyFailureMessage = stringResource(R.string.search_history_update_failed)

    LaunchedEffect(viewModel, snackbarHostState, historyFailureMessage) {
        viewModel.effects.collect { effect ->
            when (effect) {
                SearchUiEffect.HistoryUpdateFailed -> {
                    snackbarHostState.showSnackbar(historyFailureMessage)
                }
            }
        }
    }

    SearchContent(
        uiState = uiState,
        tracks = viewModel.tracks,
        artists = viewModel.artists,
        albums = viewModel.albums,
        snackbarHostState = snackbarHostState,
        onQueryChanged = viewModel::onQueryChanged,
        onSubmit = viewModel::submit,
        onClearQuery = viewModel::clearQuery,
        onScopeSelected = viewModel::onScopeSelected,
        onHistorySelected = viewModel::selectHistory,
        onDeleteHistory = viewModel::deleteHistory,
        onClearHistory = viewModel::clearHistory,
        onAlbumClick = { albumId ->
            viewModel.recordOpenedResult()
            onAlbumClick(albumId)
        },
        onArtistClick = { artistId ->
            viewModel.recordOpenedResult()
            onArtistClick(artistId)
        },
        onTrackPlay = { tracks, track ->
            viewModel.recordOpenedResult()
            playbackViewModel.playQueue(tracks = tracks, startTrack = track)
        },
        onTrackMore = onTrackMore,
        modifier = modifier,
        onBack = onBack,
    )
}

@Composable
fun SearchContent(
    uiState: SearchUiState,
    tracks: Flow<PagingData<CatalogTrackUi>>,
    artists: Flow<PagingData<CatalogArtistUi>>,
    albums: Flow<PagingData<CatalogAlbumUi>>,
    snackbarHostState: SnackbarHostState,
    onQueryChanged: (String) -> Unit,
    onSubmit: () -> Unit,
    onClearQuery: () -> Unit,
    onScopeSelected: (SearchScope) -> Unit,
    onHistorySelected: (SearchHistoryUi) -> Unit,
    onDeleteHistory: (SearchHistoryUi) -> Unit,
    onClearHistory: () -> Unit,
    onAlbumClick: (String) -> Unit,
    onArtistClick: (String) -> Unit,
    onTrackMore: (String) -> Unit,
    onTrackPlay: (List<CatalogTrackUi>, CatalogTrackUi) -> Unit,
    modifier: Modifier = Modifier,
    onBack: (() -> Unit)? = null,
) {
    var confirmClearHistory by remember { mutableStateOf(false) }
    if (confirmClearHistory) {
        AlertDialog(
            onDismissRequest = { confirmClearHistory = false },
            title = { Text(stringResource(R.string.search_clear_history_title)) },
            text = { Text(stringResource(R.string.search_clear_history_message)) },
            confirmButton = {
                TextButton(
                    onClick = {
                        confirmClearHistory = false
                        onClearHistory()
                    },
                ) {
                    Text(stringResource(R.string.common_confirm))
                }
            },
            dismissButton = {
                TextButton(onClick = { confirmClearHistory = false }) {
                    Text(stringResource(R.string.common_cancel))
                }
            },
        )
    }

    Scaffold(
        modifier = modifier.background(MaterialTheme.colorScheme.surface),
        containerColor = MaterialTheme.colorScheme.surface,
        snackbarHost = { SnackbarHost(snackbarHostState) },
    ) { contentPadding ->
        BoxWithConstraints(
            modifier =
            Modifier
                .fillMaxSize()
                .padding(contentPadding),
        ) {
            val wideLandscape = isWideLandscape(maxWidth, maxHeight)
            Column(modifier = Modifier.fillMaxSize()) {
                SearchHeader(
                    uiState = uiState,
                    wideLandscape = wideLandscape,
                    onBack = onBack,
                    onQueryChanged = onQueryChanged,
                    onSubmit = onSubmit,
                    onClearQuery = onClearQuery,
                )
                if (!uiState.isIdle || uiState.selectedScope != SearchScope.ALL) {
                    SearchScopePicker(
                        selectedScope = uiState.selectedScope,
                        onScopeSelected = onScopeSelected,
                    )
                }
                Box(
                    modifier = Modifier.weight(1f).fillMaxWidth().testTag(SearchTestTags.Results),
                    contentAlignment = Alignment.TopCenter,
                ) {
                    val resultModifier =
                        if (wideLandscape) {
                            Modifier.widthIn(max = 820.dp).fillMaxSize()
                        } else {
                            Modifier.fillMaxSize()
                        }
                    AnimatedContent(
                        targetState = uiState.toResultMode(),
                        modifier = resultModifier,
                        transitionSpec = { searchResultTransition() },
                        label = "search-result-content",
                    ) { resultMode ->
                        when (resultMode) {
                            SearchResultMode.Idle ->
                                SearchIdleContent(
                                    history = uiState.history,
                                    wideLandscape = wideLandscape,
                                    onSelect = onHistorySelected,
                                    onDelete = onDeleteHistory,
                                    onClear = { confirmClearHistory = true },
                                    onScopeSelected = onScopeSelected,
                                    modifier = Modifier.fillMaxSize(),
                                )

                            SearchResultMode.All ->
                                SearchOverview(
                                    uiState = uiState,
                                    wideLandscape = wideLandscape,
                                    onRetry = onSubmit,
                                    onScopeSelected = onScopeSelected,
                                    onAlbumClick = onAlbumClick,
                                    onArtistClick = onArtistClick,
                                    onTrackPlay = onTrackPlay,
                                    onTrackMore = onTrackMore,
                                    modifier = Modifier.fillMaxSize(),
                                )

                            SearchResultMode.Tracks -> {
                                val pagingItems = tracks.collectAsLazyPagingItems()
                                val loadedTracks = pagingItems.itemSnapshotList.items
                                CatalogPagedList(
                                    items = pagingItems,
                                    emptyTitle = stringResource(R.string.search_no_results_title),
                                    emptyMessage = stringResource(R.string.search_no_results_message),
                                    emptyIcon = Icons.Outlined.MusicNote,
                                    itemKey = CatalogTrackUi::id,
                                    itemContent = { track ->
                                        CatalogTrackRow(
                                            track = track,
                                            onClick = { onTrackPlay(loadedTracks, track) },
                                            onPlayClick = { onTrackPlay(loadedTracks, track) },
                                            onMoreClick = { onTrackMore(track.id) },
                                        )
                                    },
                                    modifier = Modifier.fillMaxSize(),
                                )
                            }

                            SearchResultMode.Artists -> {
                                val pagingItems = artists.collectAsLazyPagingItems()
                                CatalogPagedList(
                                    items = pagingItems,
                                    emptyTitle = stringResource(R.string.search_no_results_title),
                                    emptyMessage = stringResource(R.string.search_no_results_message),
                                    emptyIcon = Icons.Outlined.Person,
                                    itemKey = CatalogArtistUi::id,
                                    itemContent = { artist ->
                                        CatalogArtistRow(
                                            artist = artist,
                                            onClick = { onArtistClick(artist.id) },
                                        )
                                    },
                                    modifier = Modifier.fillMaxSize(),
                                )
                            }

                            SearchResultMode.Albums -> {
                                val pagingItems = albums.collectAsLazyPagingItems()
                                CatalogPagedList(
                                    items = pagingItems,
                                    emptyTitle = stringResource(R.string.search_no_results_title),
                                    emptyMessage = stringResource(R.string.search_no_results_message),
                                    emptyIcon = Icons.Outlined.Album,
                                    itemKey = CatalogAlbumUi::id,
                                    itemContent = { album ->
                                        CatalogAlbumRow(
                                            album = album,
                                            onClick = { onAlbumClick(album.id) },
                                        )
                                    },
                                    modifier = Modifier.fillMaxSize(),
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}
