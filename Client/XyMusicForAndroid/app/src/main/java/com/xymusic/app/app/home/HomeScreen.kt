package com.xymusic.app.app.home

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AccountCircle
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.outlined.MusicNote
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.xymusic.app.R
import com.xymusic.app.app.playback.CatalogPlaybackViewModel
import com.xymusic.app.core.ui.component.EmptyState
import com.xymusic.app.core.ui.component.ErrorState
import com.xymusic.app.core.ui.component.LoadingState
import com.xymusic.app.core.ui.component.MediaArtwork
import com.xymusic.app.core.ui.layout.isCompactLandscape
import com.xymusic.app.core.ui.layout.isWideLandscape
import com.xymusic.app.core.ui.media.CatalogAlbumShelfCard
import com.xymusic.app.core.ui.media.CatalogAlbumUi
import com.xymusic.app.core.ui.media.CatalogTrackRow
import com.xymusic.app.core.ui.media.CatalogTrackUi
import com.xymusic.app.feature.catalog.presentation.CatalogRandomUiState
import com.xymusic.app.feature.catalog.presentation.CatalogViewModel

internal object HomeTestTags {
    const val Search = "home_search"
    const val Profile = "home_profile"
    const val ProfileAvatar = "home_profile_avatar"
    const val ProfileImage = "home_profile_image"
    const val ProfilePlaceholder = "home_profile_placeholder"
    const val DiscoverList = "home_discover_list"
    const val RecommendationsPager = "home_recommendations_pager"
    const val LandscapeFeaturedPane = "home_landscape_featured_pane"
    const val LandscapeRecommendedPane = "home_landscape_recommended_pane"

    fun featuredAlbum(albumId: String): String = "home_featured_album_$albumId"
}

@Composable
fun HomeScreen(
    onTrackMore: (String) -> Unit,
    onSearchClick: () -> Unit,
    onAlbumClick: (String) -> Unit,
    modifier: Modifier = Modifier,
    onProfileClick: () -> Unit = {},
    viewModel: CatalogViewModel = hiltViewModel(),
    playbackViewModel: CatalogPlaybackViewModel = hiltViewModel(),
    profileViewModel: HomeProfileViewModel = hiltViewModel(),
) {
    val randomUiState by viewModel.randomUiState.collectAsStateWithLifecycle()
    val profileUiState by profileViewModel.uiState.collectAsStateWithLifecycle()
    val recommendationQueue =
        remember(randomUiState.recommendedTracks) {
            randomUiState.recommendedTracks.take(RECOMMENDED_TRACK_LIMIT)
        }
    HomeContent(
        randomUiState = randomUiState,
        profileUiState = profileUiState,
        onSearchClick = onSearchClick,
        onProfileClick = onProfileClick,
        onAlbumClick = onAlbumClick,
        onRecommendedTrackPlay = { track ->
            playbackViewModel.playQueue(
                tracks = recommendationQueue,
                startTrack = track,
            )
        },
        onTrackMore = onTrackMore,
        onRetryFeatured = viewModel::retryRandomAlbums,
        onRetryRecommended = viewModel::retryRandomTracks,
        modifier = modifier,
    )
}

@Composable
internal fun HomeContent(
    randomUiState: CatalogRandomUiState,
    profileUiState: HomeProfileUiState,
    onSearchClick: () -> Unit,
    onAlbumClick: (String) -> Unit,
    onRecommendedTrackPlay: (CatalogTrackUi) -> Unit,
    onTrackMore: (String) -> Unit,
    onRetryFeatured: () -> Unit,
    onRetryRecommended: () -> Unit,
    modifier: Modifier = Modifier,
    onProfileClick: () -> Unit = {},
) {
    Box(
        modifier =
        modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.surface),
    ) {
        DiscoverContent(
            randomUiState = randomUiState,
            profileUiState = profileUiState,
            onSearchClick = onSearchClick,
            onProfileClick = onProfileClick,
            onAlbumClick = onAlbumClick,
            onTrackPlay = onRecommendedTrackPlay,
            onTrackMore = onTrackMore,
            onRetryFeatured = onRetryFeatured,
            onRetryRecommended = onRetryRecommended,
            modifier = Modifier.fillMaxSize(),
        )
    }
}

@Composable
private fun DiscoverContent(
    randomUiState: CatalogRandomUiState,
    profileUiState: HomeProfileUiState,
    onSearchClick: () -> Unit,
    onProfileClick: () -> Unit,
    onAlbumClick: (String) -> Unit,
    onTrackPlay: (CatalogTrackUi) -> Unit,
    onTrackMore: (String) -> Unit,
    onRetryFeatured: () -> Unit,
    onRetryRecommended: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val featuredAlbums =
        remember(randomUiState.featuredAlbums) {
            randomUiState.featuredAlbums.take(10)
        }
    val recommendedTracks =
        remember(randomUiState.recommendedTracks) {
            randomUiState.recommendedTracks.take(RECOMMENDED_TRACK_LIMIT)
        }
    val recommendedPages =
        remember(recommendedTracks) {
            recommendedTracks.chunked(RECOMMENDED_TRACKS_PER_PAGE)
        }
    val actions =
        HomeActions(
            onSearchClick = onSearchClick,
            onProfileClick = onProfileClick,
            onAlbumClick = onAlbumClick,
            onTrackPlay = onTrackPlay,
            onTrackMore = onTrackMore,
            onRetryFeatured = onRetryFeatured,
            onRetryRecommended = onRetryRecommended,
        )

    BoxWithConstraints(modifier = modifier.fillMaxSize()) {
        if (isWideLandscape(maxWidth, maxHeight)) {
            LandscapeDiscoverContent(
                featuredAlbums = featuredAlbums,
                recommendedTracks = recommendedTracks,
                randomUiState = randomUiState,
                profileUiState = profileUiState,
                compact = isCompactLandscape(maxWidth, maxHeight),
                actions = actions,
                modifier = Modifier.fillMaxSize(),
            )
        } else {
            PortraitDiscoverContent(
                featuredAlbums = featuredAlbums,
                recommendedPages = recommendedPages,
                randomUiState = randomUiState,
                profileUiState = profileUiState,
                actions = actions,
                modifier = Modifier.fillMaxSize(),
            )
        }
    }
}

@Composable
private fun PortraitDiscoverContent(
    featuredAlbums: List<CatalogAlbumUi>,
    recommendedPages: List<List<CatalogTrackUi>>,
    randomUiState: CatalogRandomUiState,
    profileUiState: HomeProfileUiState,
    actions: HomeActions,
    modifier: Modifier = Modifier,
) {
    LazyColumn(
        modifier = modifier.testTag(HomeTestTags.DiscoverList),
        contentPadding = PaddingValues(bottom = 32.dp),
    ) {
        item(key = "home-header") {
            HomeHeader(
                profileUiState = profileUiState,
                onSearchClick = actions.onSearchClick,
                onProfileClick = actions.onProfileClick,
            )
        }
        item(key = "featured-heading") {
            HomeSectionHeader(
                title = stringResource(R.string.home_featured),
            )
        }
        when {
            featuredAlbums.isNotEmpty() ->
                item(key = "featured-albums") {
                    LazyRow(
                        contentPadding = PaddingValues(horizontal = 20.dp),
                        horizontalArrangement = Arrangement.spacedBy(14.dp),
                    ) {
                        items(featuredAlbums, key = CatalogAlbumUi::id) { album ->
                            CatalogAlbumShelfCard(
                                album = album,
                                onClick = { actions.onAlbumClick(album.id) },
                                modifier = Modifier.testTag(HomeTestTags.featuredAlbum(album.id)),
                                width = 174.dp,
                            )
                        }
                    }
                }
            randomUiState.featuredLoading ->
                item(key = "featured-loading") {
                    LoadingState(modifier = Modifier.fillMaxWidth().height(230.dp))
                }
            randomUiState.featuredFailed ->
                item(key = "featured-error") {
                    ErrorState(onRetry = actions.onRetryFeatured)
                }
            else ->
                item(key = "featured-empty") {
                    HomeEmptyContent(modifier = Modifier.height(220.dp))
                }
        }
        item(key = "tracks-heading") {
            HomeSectionHeader(
                title = stringResource(R.string.home_new_tracks),
            )
        }
        when {
            recommendedPages.isNotEmpty() ->
                item(key = "recommended-tracks") {
                    val pagerState = rememberPagerState(pageCount = { recommendedPages.size })
                    HorizontalPager(
                        state = pagerState,
                        modifier =
                        Modifier
                            .fillMaxWidth()
                            .height(RECOMMENDED_PAGE_HEIGHT)
                            .testTag(HomeTestTags.RecommendationsPager),
                        key = { page -> recommendedPages[page].first().id },
                    ) { page ->
                        Column(modifier = Modifier.fillMaxSize()) {
                            recommendedPages[page].forEach { track ->
                                CatalogTrackRow(
                                    track = track,
                                    onClick = { actions.onTrackPlay(track) },
                                    onPlayClick = { actions.onTrackPlay(track) },
                                    onMoreClick = { actions.onTrackMore(track.id) },
                                )
                            }
                        }
                    }
                }
            randomUiState.recommendedLoading ->
                item(key = "recommended-loading") {
                    LoadingState(modifier = Modifier.fillMaxWidth().height(220.dp))
                }
            randomUiState.recommendedFailed ->
                item(key = "recommended-error") {
                    ErrorState(onRetry = actions.onRetryRecommended)
                }
            else ->
                item(key = "recommended-empty") {
                    HomeEmptyContent(modifier = Modifier.height(220.dp))
                }
        }
    }
}

@Composable
private fun LandscapeDiscoverContent(
    featuredAlbums: List<CatalogAlbumUi>,
    recommendedTracks: List<CatalogTrackUi>,
    randomUiState: CatalogRandomUiState,
    profileUiState: HomeProfileUiState,
    compact: Boolean,
    actions: HomeActions,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier.testTag(HomeTestTags.DiscoverList).padding(horizontal = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        LazyColumn(
            modifier =
            Modifier
                .weight(0.9f)
                .fillMaxHeight()
                .testTag(HomeTestTags.LandscapeFeaturedPane),
            contentPadding = PaddingValues(bottom = 16.dp),
        ) {
            item(key = "home-header") {
                HomeHeader(
                    profileUiState = profileUiState,
                    onSearchClick = actions.onSearchClick,
                    onProfileClick = actions.onProfileClick,
                    compact = true,
                )
            }
            item(key = "featured-heading") {
                HomeSectionHeader(
                    title = stringResource(R.string.home_featured),
                    compact = true,
                )
            }
            when {
                featuredAlbums.isNotEmpty() ->
                    item(key = "featured-albums") {
                        LazyRow(
                            contentPadding = PaddingValues(horizontal = 8.dp),
                            horizontalArrangement = Arrangement.spacedBy(12.dp),
                        ) {
                            items(featuredAlbums, key = CatalogAlbumUi::id) { album ->
                                CatalogAlbumShelfCard(
                                    album = album,
                                    onClick = { actions.onAlbumClick(album.id) },
                                    modifier = Modifier.testTag(HomeTestTags.featuredAlbum(album.id)),
                                    width = if (compact) 132.dp else 154.dp,
                                )
                            }
                        }
                    }
                randomUiState.featuredLoading ->
                    item(key = "featured-loading") {
                        LoadingState(modifier = Modifier.fillMaxWidth().height(180.dp))
                    }
                randomUiState.featuredFailed ->
                    item(key = "featured-error") {
                        ErrorState(onRetry = actions.onRetryFeatured)
                    }
                else ->
                    item(key = "featured-empty") {
                        HomeEmptyContent(modifier = Modifier.height(170.dp))
                    }
            }
        }
        LazyColumn(
            modifier =
            Modifier
                .weight(1.1f)
                .fillMaxHeight()
                .testTag(HomeTestTags.LandscapeRecommendedPane),
            contentPadding = PaddingValues(bottom = 16.dp),
        ) {
            item(key = "tracks-heading") {
                HomeSectionHeader(
                    title = stringResource(R.string.home_new_tracks),
                    compact = true,
                )
            }
            when {
                recommendedTracks.isNotEmpty() ->
                    items(recommendedTracks, key = CatalogTrackUi::id) { track ->
                        CatalogTrackRow(
                            track = track,
                            onClick = { actions.onTrackPlay(track) },
                            onPlayClick = { actions.onTrackPlay(track) },
                            onMoreClick = { actions.onTrackMore(track.id) },
                        )
                    }
                randomUiState.recommendedLoading ->
                    item(key = "recommended-loading") {
                        LoadingState(modifier = Modifier.fillMaxWidth().height(180.dp))
                    }
                randomUiState.recommendedFailed ->
                    item(key = "recommended-error") {
                        ErrorState(onRetry = actions.onRetryRecommended)
                    }
                else ->
                    item(key = "recommended-empty") {
                        HomeEmptyContent(modifier = Modifier.height(170.dp))
                    }
            }
        }
    }
}

@Composable
private fun HomeHeader(
    profileUiState: HomeProfileUiState,
    onSearchClick: () -> Unit,
    onProfileClick: () -> Unit,
    compact: Boolean = false,
) {
    Row(
        modifier =
        Modifier
            .fillMaxWidth()
            .padding(
                start = if (compact) 8.dp else 20.dp,
                end = if (compact) 4.dp else 12.dp,
                top = if (compact) 4.dp else 16.dp,
                bottom = if (compact) 0.dp else 8.dp,
            ),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = stringResource(R.string.home_title),
            modifier = Modifier.weight(1f),
            style = if (compact) MaterialTheme.typography.headlineMedium else MaterialTheme.typography.headlineLarge,
            fontWeight = FontWeight.Bold,
        )
        IconButton(
            onClick = onSearchClick,
            modifier = Modifier.size(44.dp).testTag(HomeTestTags.Search),
        ) {
            Icon(
                imageVector = Icons.Default.Search,
                contentDescription = stringResource(R.string.navigation_search),
                tint = MaterialTheme.colorScheme.primary,
            )
        }
        val profileDescription = stringResource(R.string.navigation_mine)
        IconButton(
            onClick = onProfileClick,
            modifier =
            Modifier
                .size(44.dp)
                .testTag(HomeTestTags.Profile)
                .semantics { contentDescription = profileDescription },
        ) {
            MediaArtwork(
                url = profileUiState.avatarUrl,
                cacheKey = profileUiState.avatarCacheKey,
                contentDescription = null,
                fallbackIcon = Icons.Default.AccountCircle,
                fallbackIconFraction = 1f,
                fallbackTint = MaterialTheme.colorScheme.primary,
                fallbackModifier = Modifier.testTag(HomeTestTags.ProfilePlaceholder),
                imageModifier = Modifier.testTag(HomeTestTags.ProfileImage),
                shape = CircleShape,
                modifier = Modifier.size(29.dp).testTag(HomeTestTags.ProfileAvatar),
            )
        }
    }
}

@Composable
private fun HomeSectionHeader(title: String, compact: Boolean = false) {
    Row(
        modifier =
        Modifier
            .fillMaxWidth()
            .padding(
                start = if (compact) 8.dp else 20.dp,
                end = if (compact) 4.dp else 12.dp,
                top = if (compact) 8.dp else 20.dp,
                bottom = if (compact) 6.dp else 8.dp,
            ),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = title,
            modifier = Modifier.weight(1f),
            style = if (compact) MaterialTheme.typography.titleLarge else MaterialTheme.typography.headlineSmall,
            fontWeight = FontWeight.Bold,
        )
    }
}

@Composable
internal fun HomeEmptyContent(modifier: Modifier = Modifier) {
    Box(modifier = modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        EmptyState(
            title = stringResource(R.string.home_empty_title),
            message = stringResource(R.string.home_empty_message),
            icon = Icons.Outlined.MusicNote,
        )
    }
}

private const val RECOMMENDED_TRACK_LIMIT = 16
private const val RECOMMENDED_TRACKS_PER_PAGE = 4
private val RECOMMENDED_PAGE_HEIGHT = 260.dp

@Immutable
private data class HomeActions(
    val onSearchClick: () -> Unit,
    val onProfileClick: () -> Unit,
    val onAlbumClick: (String) -> Unit,
    val onTrackPlay: (CatalogTrackUi) -> Unit,
    val onTrackMore: (String) -> Unit,
    val onRetryFeatured: () -> Unit,
    val onRetryRecommended: () -> Unit,
)
