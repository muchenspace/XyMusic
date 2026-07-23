package com.xymusic.app.feature.catalog.presentation

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.AnimatedContentTransitionScope
import androidx.compose.animation.ContentTransform
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.togetherWith
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.outlined.Album
import androidx.compose.material.icons.outlined.MusicNote
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.PrimaryTabRow
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Tab
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.paging.PagingData
import androidx.paging.compose.collectAsLazyPagingItems
import com.xymusic.app.R
import com.xymusic.app.app.playback.CatalogPlaybackViewModel
import com.xymusic.app.core.ui.component.ErrorState
import com.xymusic.app.core.ui.component.LoadingState
import com.xymusic.app.core.ui.component.MediaArtwork
import com.xymusic.app.core.ui.layout.isCompactLandscape
import com.xymusic.app.core.ui.layout.isWideLandscape
import com.xymusic.app.core.ui.media.CachedCatalogBanner
import com.xymusic.app.core.ui.media.CatalogAlbumRow
import com.xymusic.app.core.ui.media.CatalogAlbumUi
import com.xymusic.app.core.ui.media.CatalogArtistLinks
import com.xymusic.app.core.ui.media.CatalogPagedList
import com.xymusic.app.core.ui.media.CatalogTrackRow
import com.xymusic.app.core.ui.media.CatalogTrackUi
import com.xymusic.app.ui.theme.XyMotion
import com.xymusic.app.ui.theme.spacing
import kotlinx.coroutines.flow.Flow

private const val ARTIST_TAB_SLIDE_OFFSET_DIVISOR = 24

@Composable
fun AlbumDetailRoute(
    onBack: () -> Unit,
    onTrackMore: (String) -> Unit,
    onArtistClick: (String) -> Unit,
    modifier: Modifier = Modifier,
    viewModel: AlbumDetailViewModel = hiltViewModel(),
    playbackViewModel: CatalogPlaybackViewModel = hiltViewModel(),
) {
    val uiState by viewModel.uiState.collectAsStateWithLifecycle()
    AlbumDetailScreen(
        uiState = uiState,
        tracks = viewModel.tracks,
        onBack = onBack,
        onRefresh = viewModel::refresh,
        onArtistClick = onArtistClick,
        onTrackPlay = { tracks, track ->
            playbackViewModel.playQueue(tracks = tracks, startTrack = track)
        },
        onTrackMore = onTrackMore,
        modifier = modifier,
    )
}

@Composable
fun ArtistDetailRoute(
    onBack: () -> Unit,
    onTrackMore: (String) -> Unit,
    onAlbumClick: (String) -> Unit,
    modifier: Modifier = Modifier,
    viewModel: ArtistDetailViewModel = hiltViewModel(),
    playbackViewModel: CatalogPlaybackViewModel = hiltViewModel(),
) {
    val uiState by viewModel.uiState.collectAsStateWithLifecycle()
    ArtistDetailScreen(
        uiState = uiState,
        albums = viewModel.albums,
        tracks = viewModel.tracks,
        onBack = onBack,
        onRefresh = viewModel::refresh,
        onAlbumClick = onAlbumClick,
        onTrackPlay = { tracks, track ->
            playbackViewModel.playQueue(tracks = tracks, startTrack = track)
        },
        onTrackMore = onTrackMore,
        modifier = modifier,
    )
}

@Composable
fun AlbumDetailScreen(
    uiState: CatalogDetailUiState<CatalogAlbumDetailUi>,
    tracks: Flow<PagingData<CatalogTrackUi>>,
    onBack: () -> Unit,
    onRefresh: () -> Unit,
    onArtistClick: (String) -> Unit,
    onTrackMore: (String) -> Unit,
    modifier: Modifier = Modifier,
    onTrackPlay: ((List<CatalogTrackUi>, CatalogTrackUi) -> Unit)? = null,
) {
    BoxWithConstraints(modifier = modifier) {
        val wideLandscape = isWideLandscape(maxWidth, maxHeight)
        val compactLandscape = isCompactLandscape(maxWidth, maxHeight)
        CatalogDetailScaffold(
            title = stringResource(R.string.catalog_album_detail_title),
            isRefreshing = uiState.isRefreshing,
            onBack = onBack,
            onRefresh = onRefresh,
            horizontalSafeDrawing = wideLandscape,
            modifier = Modifier.fillMaxSize(),
        ) {
            val detail = uiState.item
            when {
                detail == null && uiState.refreshFailed ->
                    ErrorState(
                        onRetry = onRefresh,
                        modifier = Modifier.fillMaxSize(),
                    )

                detail == null -> LoadingState()
                else -> {
                    val pagingItems = tracks.collectAsLazyPagingItems()
                    if (wideLandscape) {
                        AlbumLandscapeDetail(
                            detail = detail,
                            tracks = pagingItems,
                            isRefreshing = uiState.isRefreshing,
                            refreshFailed = uiState.refreshFailed,
                            compactLandscape = compactLandscape,
                            viewportHeight = maxHeight,
                            onArtistClick = onArtistClick,
                            onTrackPlay = onTrackPlay,
                            onTrackMore = onTrackMore,
                            modifier = Modifier.fillMaxSize(),
                        )
                    } else {
                        val loadedTracks = pagingItems.itemSnapshotList.items
                        CatalogPagedList(
                            items = pagingItems,
                            emptyTitle = stringResource(R.string.catalog_tracks_empty_title),
                            emptyMessage = stringResource(R.string.catalog_tracks_empty_message),
                            emptyIcon = Icons.Outlined.MusicNote,
                            itemKey = CatalogTrackUi::id,
                            itemContent = { track ->
                                CatalogTrackRow(
                                    track = track,
                                    onClick = { onTrackPlay?.invoke(loadedTracks, track) },
                                    showTrackNumber = true,
                                    onPlayClick =
                                    onTrackPlay?.let { play ->
                                        { play(loadedTracks, track) }
                                    },
                                    onMoreClick = { onTrackMore(track.id) },
                                )
                            },
                            header = {
                                Column {
                                    if (uiState.isRefreshing) {
                                        LinearProgressIndicator(
                                            modifier = Modifier.fillMaxWidth(),
                                            color = MaterialTheme.colorScheme.primary,
                                        )
                                    }
                                    if (uiState.refreshFailed) CachedCatalogBanner()
                                    AlbumDetailHeader(
                                        detail = detail,
                                        onArtistClick = onArtistClick,
                                    )
                                }
                            },
                        )
                    }
                }
            }
        }
    }
}

@Composable
fun ArtistDetailScreen(
    uiState: CatalogDetailUiState<CatalogArtistDetailUi>,
    albums: Flow<PagingData<CatalogAlbumUi>>,
    tracks: Flow<PagingData<CatalogTrackUi>>,
    onBack: () -> Unit,
    onRefresh: () -> Unit,
    onAlbumClick: (String) -> Unit,
    onTrackMore: (String) -> Unit,
    modifier: Modifier = Modifier,
    onTrackPlay: ((List<CatalogTrackUi>, CatalogTrackUi) -> Unit)? = null,
) {
    var selectedTab by rememberSaveable { mutableStateOf(ArtistDetailTab.Albums) }
    BoxWithConstraints(modifier = modifier) {
        val wideLandscape = isWideLandscape(maxWidth, maxHeight)
        val compactLandscape = isCompactLandscape(maxWidth, maxHeight)
        CatalogDetailScaffold(
            title = stringResource(R.string.catalog_artist_detail_title),
            isRefreshing = uiState.isRefreshing,
            onBack = onBack,
            onRefresh = onRefresh,
            horizontalSafeDrawing = wideLandscape,
            modifier = Modifier.fillMaxSize(),
        ) {
            val detail = uiState.item
            when {
                detail == null && uiState.refreshFailed ->
                    ErrorState(
                        onRetry = onRefresh,
                        modifier = Modifier.fillMaxSize(),
                    )

                detail == null -> LoadingState()
                wideLandscape ->
                    ArtistLandscapeDetail(
                        detail = detail,
                        selectedTab = selectedTab,
                        onTabSelected = { selectedTab = it },
                        isRefreshing = uiState.isRefreshing,
                        refreshFailed = uiState.refreshFailed,
                        viewportHeight = maxHeight,
                        modifier = Modifier.fillMaxSize(),
                    ) {
                        ArtistLandscapeCollection(
                            selectedTab = selectedTab,
                            albums = albums,
                            tracks = tracks,
                            compactLandscape = compactLandscape,
                            onAlbumClick = onAlbumClick,
                            onTrackMore = onTrackMore,
                            onTrackPlay = onTrackPlay,
                        )
                    }

                else ->
                    ArtistDetailCollection(
                        detail = detail,
                        selectedTab = selectedTab,
                        albums = albums,
                        tracks = tracks,
                        isRefreshing = uiState.isRefreshing,
                        refreshFailed = uiState.refreshFailed,
                        onTabSelected = { selectedTab = it },
                        onAlbumClick = onAlbumClick,
                        onTrackMore = onTrackMore,
                        onTrackPlay = onTrackPlay,
                    )
            }
        }
    }
}

@Composable
private fun ArtistDetailCollection(
    detail: CatalogArtistDetailUi,
    selectedTab: ArtistDetailTab,
    albums: Flow<PagingData<CatalogAlbumUi>>,
    tracks: Flow<PagingData<CatalogTrackUi>>,
    isRefreshing: Boolean,
    refreshFailed: Boolean,
    onTabSelected: (ArtistDetailTab) -> Unit,
    onAlbumClick: (String) -> Unit,
    onTrackMore: (String) -> Unit,
    onTrackPlay: ((List<CatalogTrackUi>, CatalogTrackUi) -> Unit)?,
) {
    AnimatedContent(
        targetState = selectedTab,
        transitionSpec = { artistDetailTabContentTransition() },
        label = "artist-detail-collection",
    ) { tab ->
        when (tab) {
            ArtistDetailTab.Albums -> {
                val pagingItems = albums.collectAsLazyPagingItems()
                CatalogPagedList(
                    items = pagingItems,
                    emptyTitle = stringResource(R.string.catalog_albums_empty_title),
                    emptyMessage = stringResource(R.string.catalog_albums_empty_message),
                    emptyIcon = Icons.Outlined.Album,
                    itemKey = CatalogAlbumUi::id,
                    itemContent = { album ->
                        CatalogAlbumRow(
                            album = album,
                            onClick = { onAlbumClick(album.id) },
                        )
                    },
                    header = {
                        ArtistDetailHeader(
                            detail = detail,
                            selectedTab = selectedTab,
                            onTabSelected = onTabSelected,
                            isRefreshing = isRefreshing,
                            refreshFailed = refreshFailed,
                        )
                    },
                )
            }

            ArtistDetailTab.Tracks -> {
                val pagingItems = tracks.collectAsLazyPagingItems()
                val loadedTracks = pagingItems.itemSnapshotList.items
                CatalogPagedList(
                    items = pagingItems,
                    emptyTitle = stringResource(R.string.catalog_tracks_empty_title),
                    emptyMessage = stringResource(R.string.catalog_tracks_empty_message),
                    emptyIcon = Icons.Outlined.MusicNote,
                    itemKey = CatalogTrackUi::id,
                    itemContent = { track ->
                        CatalogTrackRow(
                            track = track,
                            onClick = { onTrackPlay?.invoke(loadedTracks, track) },
                            onPlayClick =
                            onTrackPlay?.let { play ->
                                { play(loadedTracks, track) }
                            },
                            onMoreClick = { onTrackMore(track.id) },
                        )
                    },
                    header = {
                        ArtistDetailHeader(
                            detail = detail,
                            selectedTab = selectedTab,
                            onTabSelected = onTabSelected,
                            isRefreshing = isRefreshing,
                            refreshFailed = refreshFailed,
                        )
                    },
                )
            }
        }
    }
}

internal fun AnimatedContentTransitionScope<ArtistDetailTab>.artistDetailTabContentTransition(): ContentTransform {
    val slideDirection = if (targetState.ordinal > initialState.ordinal) 1 else -1
    return (
        slideInHorizontally(
            animationSpec = tween(XyMotion.Quick, easing = XyMotion.NavigationEasing),
            initialOffsetX = { fullWidth -> fullWidth / ARTIST_TAB_SLIDE_OFFSET_DIVISOR * slideDirection },
        ) + fadeIn(tween(XyMotion.Quick, easing = XyMotion.NavigationEasing))
    ).togetherWith(
        slideOutHorizontally(
            animationSpec = tween(XyMotion.Quick, easing = XyMotion.NavigationEasing),
            targetOffsetX = { fullWidth -> -(fullWidth / ARTIST_TAB_SLIDE_OFFSET_DIVISOR) * slideDirection },
        ) + fadeOut(tween(XyMotion.Quick, easing = XyMotion.NavigationEasing)),
    )
}

@Composable
private fun AlbumDetailHeader(detail: CatalogAlbumDetailUi, onArtistClick: (String) -> Unit) {
    val album = detail.album
    val colorScheme = MaterialTheme.colorScheme
    Column(
        modifier =
        Modifier
            .fillMaxWidth()
            .padding(MaterialTheme.spacing.contentPadding),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(MaterialTheme.spacing.compact),
    ) {
        MediaArtwork(
            url = album.cover?.url,
            cacheKey = album.cover?.cacheKey,
            contentDescription = null,
            fallbackIcon = Icons.Outlined.Album,
            modifier = Modifier.size(260.dp),
            elevation = 8.dp,
        )
        Text(
            text = album.title,
            modifier = Modifier.fillMaxWidth(),
            textAlign = TextAlign.Start,
            style = MaterialTheme.typography.headlineLarge,
            fontWeight = FontWeight.Bold,
        )
        CatalogArtistLinks(
            artists = album.artists,
            onArtistClick = onArtistClick,
        )
        Row(
            horizontalArrangement = Arrangement.spacedBy(MaterialTheme.spacing.medium),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            album.releaseDate?.let {
                Text(
                    text = it,
                    color = colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.bodyMedium,
                )
            }
            Text(
                text = stringResource(R.string.catalog_track_count, album.trackCount),
                color = colorScheme.onSurfaceVariant,
                style = MaterialTheme.typography.bodyMedium,
            )
        }
        Text(
            text =
            detail.description?.takeIf(String::isNotBlank)
                ?: stringResource(R.string.catalog_no_description),
            modifier = Modifier.fillMaxWidth(),
            color = colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.bodyMedium,
            textAlign = TextAlign.Start,
        )
        Spacer(modifier = Modifier.height(MaterialTheme.spacing.extraSmall))
        Text(
            text = stringResource(R.string.catalog_album_tracks),
            modifier =
            Modifier
                .fillMaxWidth()
                .padding(top = MaterialTheme.spacing.small),
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.Bold,
        )
        HorizontalDivider(color = colorScheme.outlineVariant)
    }
}

@Composable
private fun ArtistDetailHeader(
    detail: CatalogArtistDetailUi,
    selectedTab: ArtistDetailTab,
    onTabSelected: (ArtistDetailTab) -> Unit,
    isRefreshing: Boolean,
    refreshFailed: Boolean,
) {
    val colorScheme = MaterialTheme.colorScheme
    Column(
        modifier = Modifier.fillMaxWidth(),
    ) {
        if (isRefreshing) LinearProgressIndicator(modifier = Modifier.fillMaxWidth(), color = colorScheme.primary)
        if (refreshFailed) CachedCatalogBanner()
        Column(
            modifier =
            Modifier
                .fillMaxWidth()
                .padding(MaterialTheme.spacing.contentPadding),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(MaterialTheme.spacing.compact),
        ) {
            MediaArtwork(
                url = detail.artist.artwork?.url,
                cacheKey = detail.artist.artwork?.cacheKey,
                contentDescription = null,
                fallbackIcon = Icons.Outlined.Person,
                shape = CircleShape,
                modifier = Modifier.size(200.dp),
                elevation = 8.dp,
            )
            Text(
                text = detail.artist.name,
                modifier = Modifier.fillMaxWidth(),
                textAlign = TextAlign.Center,
                style = MaterialTheme.typography.headlineMedium,
                fontWeight = FontWeight.Bold,
            )
            Text(
                text =
                detail.description?.takeIf(String::isNotBlank)
                    ?: stringResource(R.string.catalog_no_description),
                modifier = Modifier.fillMaxWidth(),
                color = colorScheme.onSurfaceVariant,
                style = MaterialTheme.typography.bodyMedium,
                textAlign = TextAlign.Center,
            )
        }
        PrimaryTabRow(
            selectedTabIndex = selectedTab.ordinal,
            containerColor = Color.Transparent,
            contentColor = colorScheme.onSurface,
            divider = {
                HorizontalDivider(color = colorScheme.outlineVariant)
            },
        ) {
            ArtistDetailTab.entries.forEach { tab ->
                Tab(
                    selected = selectedTab == tab,
                    onClick = { onTabSelected(tab) },
                    modifier = Modifier.testTag(CatalogDetailTestTags.artistTab(tab)),
                    text = {
                        Text(
                            stringResource(
                                if (tab == ArtistDetailTab.Albums) {
                                    R.string.catalog_artist_albums
                                } else {
                                    R.string.catalog_artist_tracks
                                },
                            ),
                            style = MaterialTheme.typography.labelMedium,
                        )
                    },
                )
            }
        }
    }
}

@Composable
private fun CatalogDetailScaffold(
    title: String,
    isRefreshing: Boolean,
    onBack: () -> Unit,
    onRefresh: () -> Unit,
    horizontalSafeDrawing: Boolean,
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    Scaffold(
        modifier =
        modifier
            .background(MaterialTheme.colorScheme.background)
            .then(
                if (horizontalSafeDrawing) {
                    Modifier.windowInsetsPadding(
                        WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal),
                    )
                } else {
                    Modifier
                },
            ),
        topBar = {
            Row(
                modifier =
                Modifier
                    .fillMaxWidth()
                    .padding(MaterialTheme.spacing.extraSmall),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                IconButton(onClick = onBack, modifier = Modifier.size(40.dp)) {
                    Icon(
                        imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                        contentDescription = stringResource(R.string.common_back),
                        tint = MaterialTheme.colorScheme.onSurface,
                    )
                }
                Text(
                    text = title,
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.SemiBold,
                    modifier = Modifier.weight(1f),
                )
                IconButton(
                    onClick = onRefresh,
                    enabled = !isRefreshing,
                    modifier = Modifier.size(40.dp),
                ) {
                    if (isRefreshing) {
                        CircularProgressIndicator(
                            modifier = Modifier.size(20.dp),
                            strokeWidth = 2.dp,
                        )
                    } else {
                        Icon(
                            imageVector = Icons.Default.Refresh,
                            contentDescription = stringResource(R.string.catalog_refresh),
                            tint = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
        },
    ) { contentPadding ->
        Box(
            modifier =
            Modifier
                .fillMaxSize()
                .padding(contentPadding),
        ) {
            content()
        }
    }
}
