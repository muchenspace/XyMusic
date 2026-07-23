package com.xymusic.app.app.navigation

import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.NavHostController
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import com.xymusic.app.app.trackactions.TrackActionsSheet
import com.xymusic.app.app.trackactions.TrackActionsUiEffect
import com.xymusic.app.app.trackactions.TrackActionsViewModel
import com.xymusic.app.core.network.ServerEndpoint
import com.xymusic.app.core.ui.layout.isCompactLandscape
import com.xymusic.app.feature.player.presentation.PlayerUiEffect
import com.xymusic.app.feature.player.presentation.PlayerViewModel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map

@Composable
fun MainNavigation(
    snackbarHostState: SnackbarHostState,
    dynamicColorEnabled: Boolean,
    onDynamicColorChanged: (Boolean) -> Unit,
    serverEndpoint: ServerEndpoint,
    onServerEndpointChanged: (ServerEndpoint) -> Unit,
    modifier: Modifier = Modifier,
) {
    val navController = rememberNavController()
    val currentBackStackEntry by navController.currentBackStackEntryAsState()
    val currentRoute = currentBackStackEntry?.destination?.route
    val selectedMainDestination = MainDestination.fromRoute(currentRoute)
    var lastSelectedMainDestination by remember { mutableStateOf(MainDestination.Home) }
    SideEffect {
        selectedMainDestination?.let { lastSelectedMainDestination = it }
    }
    val resources = LocalResources.current
    val playerViewModel: PlayerViewModel = hiltViewModel()
    val trackActionsViewModel: TrackActionsViewModel = hiltViewModel()
    val visibleEntries by navController.visibleEntries.collectAsStateWithLifecycle()
    val playerIsFavoriteFlow =
        remember(trackActionsViewModel) {
            trackActionsViewModel.uiState
                .map { state -> state.playerIsFavorite }
                .distinctUntilChanged()
        }
    val playerIsFavorite by
        playerIsFavoriteFlow.collectAsStateWithLifecycle(
            initialValue = trackActionsViewModel.uiState.value.playerIsFavorite,
        )
    val hasPlayerItemFlow =
        remember(playerViewModel) {
            playerViewModel.uiState
                .map { state -> state.player.currentItem != null }
                .distinctUntilChanged()
        }
    val hasPlayerItem by
        hasPlayerItemFlow.collectAsStateWithLifecycle(
            initialValue = playerViewModel.uiState.value.player.currentItem != null,
        )

    PlayerEffectSnackbar(playerViewModel.effects, snackbarHostState)
    LaunchedEffect(trackActionsViewModel, snackbarHostState, resources) {
        trackActionsViewModel.effects.collect { effect ->
            when (effect) {
                is TrackActionsUiEffect.ShowMessage -> {
                    snackbarHostState.showSnackbar(resources.getString(effect.messageRes))
                }
            }
        }
    }
    LaunchedEffect(playerViewModel, trackActionsViewModel) {
        playerViewModel.uiState
            .map { state -> state.player.currentItem?.trackId }
            .distinctUntilChanged()
            .collect { trackId -> trackActionsViewModel.setPlayerTrack(trackId) }
    }

    BoxWithConstraints(modifier = modifier) {
        val compactLandscape = isCompactLandscape(maxWidth, maxHeight)
        val layoutConfig =
            MainNavigationLayoutConfig(
                useNavigationRail = maxWidth >= 600.dp,
                compactPlayerBar = compactLandscape,
                hasPlayerItem = hasPlayerItem,
            )
        val playerEntryStillVisible =
            visibleEntries.any { entry -> entry.destination.route == PlayerDestination.NowPlaying.route }
        val chromeState =
            mainNavigationChromeState(
                config = layoutConfig,
                currentRoute = currentRoute,
                lastSelectedMainDestination = lastSelectedMainDestination,
            )
        val navigateMain: (MainDestination) -> Unit = { destination ->
            navController.navigateMain(destination.route)
        }

        MainNavigationLayout(
            config = layoutConfig,
            chromeState = chromeState,
            playerEntryStillVisible = playerEntryStillVisible,
            snackbarHostState = snackbarHostState,
            navigationRail = {
                MainNavigationRail(
                    currentDestination = chromeState.selectedMainDestination,
                    onDestinationSelected = navigateMain,
                )
            },
            bottomNavigation = {
                GlassNavigationBar(
                    currentDestination = chromeState.selectedMainDestination,
                    onDestinationSelected = navigateMain,
                )
            },
            miniPlayer = { miniPlayerModifier ->
                PlayerMiniBarRoute(
                    playerViewModel = playerViewModel,
                    onOpenPlayer = {
                        navController.navigate(PlayerDestination.NowPlaying.route) {
                            launchSingleTop = true
                        }
                    },
                    compact = layoutConfig.compactPlayerBar,
                    modifier = miniPlayerModifier,
                )
            },
        ) {
            MainNavHost(
                navController = navController,
                playerViewModel = playerViewModel,
                playerIsFavorite = playerIsFavorite,
                onTrackMore = trackActionsViewModel::open,
                onTogglePlayerFavorite = trackActionsViewModel::togglePlayerFavorite,
                dynamicColorEnabled = dynamicColorEnabled,
                onDynamicColorChanged = onDynamicColorChanged,
                serverEndpoint = serverEndpoint,
                onServerEndpointChanged = onServerEndpointChanged,
                layoutConfig = layoutConfig,
                modifier = Modifier.fillMaxSize(),
            )
        }
    }
    TrackActionsSheetHost(viewModel = trackActionsViewModel)
}

@Composable
internal fun PlayerEffectSnackbar(effects: Flow<PlayerUiEffect>, snackbarHostState: SnackbarHostState) {
    val resources = LocalResources.current
    LaunchedEffect(effects, snackbarHostState, resources) {
        effects.collect { effect ->
            when (effect) {
                is PlayerUiEffect.ShowMessage ->
                    snackbarHostState.showSnackbar(resources.getString(effect.messageRes))
            }
        }
    }
}

internal fun NavHostController.navigateMain(route: String, restoreState: Boolean = true) {
    navigate(route) {
        popUpTo(graph.findStartDestination().id) {
            saveState = true
        }
        launchSingleTop = true
        this.restoreState = restoreState
    }
}

@Composable
private fun TrackActionsSheetHost(viewModel: TrackActionsViewModel) {
    val uiState by viewModel.uiState.collectAsStateWithLifecycle()
    TrackActionsSheet(
        uiState = uiState,
        onDismiss = viewModel::dismiss,
        onToggleFavorite = viewModel::toggleSelectedFavorite,
        onAddToPlaylist = viewModel::addToPlaylist,
        onCreatePlaylistAndAdd = viewModel::createPlaylistAndAdd,
        onDownload = viewModel::downloadSelected,
        onRemoveDownload = viewModel::removeSelectedDownload,
    )
}
