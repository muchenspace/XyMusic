package com.xymusic.app.app.navigation

import androidx.compose.animation.ExitTransition
import androidx.compose.runtime.Composable
import androidx.compose.runtime.State
import androidx.compose.ui.Modifier
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import com.xymusic.app.app.home.HomeScreen
import com.xymusic.app.app.mine.MineScreen
import com.xymusic.app.core.network.ServerEndpoint
import com.xymusic.app.feature.catalog.presentation.AlbumDetailRoute
import com.xymusic.app.feature.catalog.presentation.ArtistDetailRoute
import com.xymusic.app.feature.library.presentation.LibraryScreen
import com.xymusic.app.feature.library.presentation.LibraryTab
import com.xymusic.app.feature.player.presentation.PlayerScreen
import com.xymusic.app.feature.player.presentation.PlayerUiState
import com.xymusic.app.feature.player.presentation.PlayerViewModel
import com.xymusic.app.feature.playlist.presentation.PlaylistRoute
import com.xymusic.app.feature.playlist.presentation.PlaylistRouteArgs
import com.xymusic.app.feature.search.presentation.SearchScreen
import com.xymusic.app.feature.settings.presentation.SettingsScreen
import com.xymusic.app.ui.theme.playerReturnInto
import com.xymusic.app.ui.theme.playerSlideInto
import com.xymusic.app.ui.theme.slideFadeBackInto
import com.xymusic.app.ui.theme.slideFadeBackOutOf
import com.xymusic.app.ui.theme.slideFadeInto
import com.xymusic.app.ui.theme.slideFadeOutOf

private const val LIBRARY_TAB_ARGUMENT = "libraryTab"
private val LIBRARY_ROUTE =
    "${MainSecondaryDestination.Library.route}?$LIBRARY_TAB_ARGUMENT={$LIBRARY_TAB_ARGUMENT}"

@Composable
internal fun MainNavHost(
    navController: NavHostController,
    playerViewModel: PlayerViewModel,
    playerUiState: PlayerUiState,
    playbackPosition: State<Float>,
    playerIsFavorite: Boolean,
    onTrackMore: (String) -> Unit,
    onTogglePlayerFavorite: () -> Unit,
    dynamicColorEnabled: Boolean,
    onDynamicColorChanged: (Boolean) -> Unit,
    serverEndpoint: ServerEndpoint,
    onServerEndpointChanged: (ServerEndpoint) -> Unit,
    layoutConfig: MainNavigationLayoutConfig,
    chromeInsets: MainNavigationChromeInsets,
    modifier: Modifier = Modifier,
) {
    NavHost(
        navController = navController,
        startDestination = MainDestination.Home.route,
        modifier = modifier,
        enterTransition = {
            if (targetState.destination.route == PlayerDestination.NowPlaying.route) {
                playerSlideInto()
            } else {
                slideFadeInto()
            }
        },
        exitTransition = {
            if (targetState.destination.route == PlayerDestination.NowPlaying.route) {
                ExitTransition.None
            } else {
                slideFadeOutOf()
            }
        },
        popEnterTransition = {
            when {
                targetState.destination.route == PlayerDestination.NowPlaying.route -> playerSlideInto()
                initialState.destination.route == PlayerDestination.NowPlaying.route -> playerReturnInto()
                else -> slideFadeBackInto()
            }
        },
        popExitTransition = {
            if (initialState.destination.route == PlayerDestination.NowPlaying.route) {
                ExitTransition.KeepUntilTransitionsFinished
            } else {
                slideFadeBackOutOf()
            }
        },
    ) {
        composable(route = MainDestination.Home.route) {
            MainNavigationRouteLayout(
                layout = MainNavigationContentLayout.Primary,
                config = layoutConfig,
                chromeInsets = chromeInsets,
            ) {
                HomeScreen(
                    onTrackMore = onTrackMore,
                    onAlbumClick = { albumId ->
                        navController.navigate(CatalogDestination.AlbumDetail.createRoute(albumId))
                    },
                    onSearchClick = {
                        navController.navigate(MainSecondaryDestination.Search.route) {
                            launchSingleTop = true
                        }
                    },
                    onProfileClick = {
                        navController.navigateMain(MainDestination.Mine.route)
                    },
                )
            }
        }
        composable(route = MainSecondaryDestination.Search.route) {
            MainNavigationRouteLayout(
                layout = MainNavigationContentLayout.Secondary,
                config = layoutConfig,
                chromeInsets = chromeInsets,
            ) {
                SearchScreen(
                    onTrackMore = onTrackMore,
                    onAlbumClick = { navController.navigate(CatalogDestination.AlbumDetail.createRoute(it)) },
                    onArtistClick = { navController.navigate(CatalogDestination.ArtistDetail.createRoute(it)) },
                    onBack = navController::navigateUp,
                )
            }
        }
        composable(
            route = LIBRARY_ROUTE,
            arguments =
            listOf(
                navArgument(LIBRARY_TAB_ARGUMENT) {
                    type = NavType.StringType
                    defaultValue = LibraryTab.Favorites.name
                },
            ),
        ) { entry ->
            val initialTab =
                entry.arguments
                    ?.getString(LIBRARY_TAB_ARGUMENT)
                    ?.let { value -> LibraryTab.entries.firstOrNull { it.name == value } }
                    ?: LibraryTab.Favorites
            MainNavigationRouteLayout(
                layout = MainNavigationContentLayout.Secondary,
                config = layoutConfig,
                chromeInsets = chromeInsets,
            ) {
                LibraryScreen(
                    onTrackMore = onTrackMore,
                    onPlaylistClick = { navController.navigate(PlaylistDestination.Detail.createRoute(it)) },
                    onBack = navController::navigateUp,
                    initialTab = initialTab,
                )
            }
        }
        composable(route = MainDestination.Mine.route) {
            MainNavigationRouteLayout(
                layout = MainNavigationContentLayout.Primary,
                config = layoutConfig,
                chromeInsets = chromeInsets,
            ) {
                MineScreen(
                    onPlaylistClick = { navController.navigate(PlaylistDestination.Detail.createRoute(it)) },
                    onOpenLibrary = { tab ->
                        navController.navigate(libraryRoute(tab)) {
                            launchSingleTop = true
                        }
                    },
                    onOpenSettings = {
                        navController.navigate(MainSecondaryDestination.Settings.route) {
                            launchSingleTop = true
                        }
                    },
                )
            }
        }
        composable(route = MainSecondaryDestination.Settings.route) {
            MainNavigationRouteLayout(
                layout = MainNavigationContentLayout.Secondary,
                config = layoutConfig,
                chromeInsets = chromeInsets,
            ) {
                SettingsScreen(
                    dynamicColorEnabled = dynamicColorEnabled,
                    onDynamicColorChanged = onDynamicColorChanged,
                    serverEndpoint = serverEndpoint,
                    onServerEndpointChanged = onServerEndpointChanged,
                    onBack = navController::navigateUp,
                )
            }
        }
        composable(route = PlayerDestination.NowPlaying.route) {
            MainNavigationRouteLayout(
                layout = MainNavigationContentLayout.FullScreen,
                config = layoutConfig,
                chromeInsets = chromeInsets,
            ) {
                PlayerScreenRoute(
                    playerViewModel = playerViewModel,
                    uiState = playerUiState,
                    playbackPosition = playbackPosition,
                    onBack = navController::navigateUp,
                    isFavorite = playerIsFavorite,
                    onToggleFavorite = onTogglePlayerFavorite,
                    onAddToPlaylist = onTrackMore,
                )
            }
        }
        composable(
            route = PlaylistDestination.Detail.route,
            arguments = listOf(navArgument(PlaylistRouteArgs.PlaylistId) { type = NavType.StringType }),
        ) {
            MainNavigationRouteLayout(
                layout = MainNavigationContentLayout.EdgeToEdge,
                config = layoutConfig,
                chromeInsets = chromeInsets,
            ) {
                PlaylistRoute(
                    onBack = navController::navigateUp,
                    onDeleted = navController::navigateUp,
                    onTrackMore = onTrackMore,
                )
            }
        }
        composable(
            route = CatalogDestination.AlbumDetail.route,
            arguments =
            listOf(
                navArgument(CatalogDestination.AlbumDetail.argumentName) { type = NavType.StringType },
            ),
        ) {
            MainNavigationRouteLayout(
                layout = MainNavigationContentLayout.Secondary,
                config = layoutConfig,
                chromeInsets = chromeInsets,
            ) {
                AlbumDetailRoute(
                    onBack = navController::navigateUp,
                    onTrackMore = onTrackMore,
                    onArtistClick = { navController.navigate(CatalogDestination.ArtistDetail.createRoute(it)) },
                )
            }
        }
        composable(
            route = CatalogDestination.ArtistDetail.route,
            arguments =
            listOf(
                navArgument(CatalogDestination.ArtistDetail.argumentName) { type = NavType.StringType },
            ),
        ) {
            MainNavigationRouteLayout(
                layout = MainNavigationContentLayout.Secondary,
                config = layoutConfig,
                chromeInsets = chromeInsets,
            ) {
                ArtistDetailRoute(
                    onBack = navController::navigateUp,
                    onTrackMore = onTrackMore,
                    onAlbumClick = { navController.navigate(CatalogDestination.AlbumDetail.createRoute(it)) },
                )
            }
        }
    }
}

@Composable
private fun PlayerScreenRoute(
    playerViewModel: PlayerViewModel,
    uiState: PlayerUiState,
    playbackPosition: State<Float>,
    onBack: () -> Unit,
    isFavorite: Boolean,
    onToggleFavorite: () -> Unit,
    onAddToPlaylist: (String) -> Unit,
) {
    PlayerScreen(
        uiState = uiState,
        onBack = onBack,
        onTogglePlayback = playerViewModel::togglePlayback,
        onSeek = playerViewModel::seekTo,
        onPrevious = playerViewModel::skipToPrevious,
        onNext = playerViewModel::skipToNext,
        onCyclePlaybackMode = playerViewModel::cyclePlaybackMode,
        onSelectQueueItem = playerViewModel::selectQueueItem,
        onRemoveQueueItem = playerViewModel::removeQueueItem,
        onMoveQueueItem = playerViewModel::moveQueueItem,
        onClearQueue = playerViewModel::clearQueue,
        onPlaybackSpeedChange = playerViewModel::setPlaybackSpeed,
        onSleepTimerChange = playerViewModel::setSleepTimer,
        isFavorite = isFavorite,
        onToggleFavorite = onToggleFavorite,
        onAddToPlaylist = {
            uiState.player.currentItem
                ?.trackId
                ?.let(onAddToPlaylist)
        },
        playbackPosition = playbackPosition,
    )
}

private fun libraryRoute(tab: LibraryTab): String =
    "${MainSecondaryDestination.Library.route}?$LIBRARY_TAB_ARGUMENT=${tab.name}"
