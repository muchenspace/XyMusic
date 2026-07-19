package com.xymusic.app.app.navigation

import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.material3.Scaffold
import androidx.compose.material3.ScaffoldDefaults
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
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
import com.xymusic.app.core.ui.layout.isWideLandscape
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
    val currentDestination = MainDestination.fromRoute(currentRoute)
    val resources = LocalResources.current
    val playerViewModel: PlayerViewModel = hiltViewModel()
    val trackActionsViewModel: TrackActionsViewModel = hiltViewModel()
    val playerIsFavorite = playerFavoriteState(trackActionsViewModel)

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
        val wideLandscape = isWideLandscape(maxWidth, maxHeight)
        val compactLandscape = isCompactLandscape(maxWidth, maxHeight)
        val useNavigationRail = maxWidth >= 600.dp
        val showMainChrome = shouldShowMainBottomBar(currentRoute)
        val showNavigationRail = useNavigationRail && showMainChrome
        val showBottomNavigation = !useNavigationRail && showMainChrome
        val edgeToEdgeDetail =
            currentRoute == PlayerDestination.NowPlaying.route ||
                currentRoute == PlaylistDestination.Detail.route
        val contentWindowInsets =
            when {
                edgeToEdgeDetail -> WindowInsets(0, 0, 0, 0)
                showNavigationRail ->
                    WindowInsets.safeDrawing.only(
                        WindowInsetsSides.Top + WindowInsetsSides.End + WindowInsetsSides.Bottom,
                    )
                wideLandscape -> WindowInsets.safeDrawing
                else -> ScaffoldDefaults.contentWindowInsets
            }
        val miniBarModifier =
            when {
                showBottomNavigation ->
                    Modifier.windowInsetsPadding(
                        WindowInsets.navigationBars.only(WindowInsetsSides.Horizontal),
                    )
                showNavigationRail ->
                    Modifier.windowInsetsPadding(
                        WindowInsets.navigationBars.only(
                            WindowInsetsSides.End + WindowInsetsSides.Bottom,
                        ),
                    )
                else -> Modifier.windowInsetsPadding(WindowInsets.navigationBars)
            }
        val navigateMain: (MainDestination) -> Unit = { destination ->
            navController.navigateMain(destination.route)
        }

        Row(modifier = Modifier.fillMaxSize()) {
            if (showNavigationRail) {
                MainNavigationRail(
                    currentDestination = currentDestination,
                    onDestinationSelected = navigateMain,
                )
            }
            Scaffold(
                modifier = Modifier.weight(1f).fillMaxHeight(),
                contentWindowInsets = contentWindowInsets,
                snackbarHost = { SnackbarHost(hostState = snackbarHostState) },
                bottomBar = {
                    Column {
                        if (currentRoute != PlayerDestination.NowPlaying.route) {
                            PlayerMiniBarRoute(
                                playerViewModel = playerViewModel,
                                onOpenPlayer = {
                                    navController.navigate(PlayerDestination.NowPlaying.route) {
                                        launchSingleTop = true
                                    }
                                },
                                compact = compactLandscape,
                                modifier = miniBarModifier,
                            )
                        }
                        if (showBottomNavigation) {
                            GlassNavigationBar(
                                currentDestination = currentDestination,
                                onDestinationSelected = navigateMain,
                            )
                        }
                    }
                },
            ) { contentPadding ->
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
                    modifier =
                    Modifier
                        .fillMaxSize()
                        .padding(contentPadding)
                        .consumeWindowInsets(contentPadding),
                )
            }
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

@Composable
private fun playerFavoriteState(viewModel: TrackActionsViewModel): Boolean {
    val uiState by viewModel.uiState.collectAsStateWithLifecycle()
    return uiState.playerIsFavorite
}
