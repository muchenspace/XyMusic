package com.xymusic.app.app.navigation

import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.isImeVisible
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.State
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
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
import com.xymusic.app.app.playback.CatalogPlaybackViewModel
import com.xymusic.app.app.trackactions.TrackActionsSheet
import com.xymusic.app.app.trackactions.TrackActionsUiEffect
import com.xymusic.app.app.trackactions.TrackActionsUiState
import com.xymusic.app.app.trackactions.TrackActionsViewModel
import com.xymusic.app.core.ui.layout.isCompactLandscape
import com.xymusic.app.domain.server.ServerEndpoint
import com.xymusic.app.feature.player.presentation.PlayerScreen
import com.xymusic.app.feature.player.presentation.PlayerUiEffect
import com.xymusic.app.feature.player.presentation.PlayerUiState
import com.xymusic.app.feature.player.presentation.PlayerViewModel
import com.xymusic.app.feature.player.presentation.rememberPlaybackPositionState
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map

@OptIn(ExperimentalLayoutApi::class)
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
    val catalogPlaybackViewModel: CatalogPlaybackViewModel = hiltViewModel()
    val trackActionsViewModel: TrackActionsViewModel = hiltViewModel()
    val playerUiState by playerViewModel.structuralUiState.collectAsStateWithLifecycle()
    val playbackPosition = rememberPlaybackPositionState(playerViewModel.playbackState)
    val trackActionsUiState by trackActionsViewModel.uiState.collectAsStateWithLifecycle()
    val playerIsFavorite = trackActionsUiState.playerIsFavorite
    val hasPlayerItem = playerUiState.player.currentItem != null
    val imeVisible = WindowInsets.isImeVisible
    var playerPresented by rememberSaveable { mutableStateOf(false) }

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
        playerViewModel.structuralUiState
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
        val chromeState =
            mainNavigationChromeState(
                config = layoutConfig,
                currentRoute = currentRoute,
                lastSelectedMainDestination = lastSelectedMainDestination,
                imeVisible = imeVisible,
            )
        val navigateMain: (MainDestination) -> Unit = { destination ->
            navController.navigateMain(destination.route)
        }

        MainNavigationLayout(
            config = layoutConfig,
            chromeState = chromeState,
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
                if (!imeVisible) {
                    PlayerMiniBarRoute(
                        uiState = playerUiState,
                        playbackPosition = playbackPosition,
                        onOpenPlayer = {
                            playerPresented = true
                        },
                        onTogglePlayback = playerViewModel::togglePlayback,
                        onNext = playerViewModel::skipToNext,
                        compact = layoutConfig.compactPlayerBar,
                        modifier = miniPlayerModifier,
                    )
                }
            },
        ) { chromeInsets ->
            MainNavHost(
                navController = navController,
                onTrackPlay = { tracks, track ->
                    catalogPlaybackViewModel.playQueue(tracks = tracks, startTrack = track)
                },
                onTrackMore = trackActionsViewModel::open,
                dynamicColorEnabled = dynamicColorEnabled,
                onDynamicColorChanged = onDynamicColorChanged,
                serverEndpoint = serverEndpoint,
                onServerEndpointChanged = onServerEndpointChanged,
                layoutConfig = layoutConfig,
                chromeInsets = chromeInsets,
                modifier = Modifier.fillMaxSize(),
            )
        }
        PlayerTransitionOverlay(
            visible = playerPresented,
            onDismissRequest = { playerPresented = false },
            modifier = Modifier.fillMaxSize(),
        ) { onDismiss, dismissGestureModifier, immersiveLandscape ->
            PlayerOverlayContent(
                uiState = playerUiState,
                playerViewModel = playerViewModel,
                playbackPosition = playbackPosition,
                onBack = onDismiss,
                isFavorite = playerIsFavorite,
                onToggleFavorite = trackActionsViewModel::togglePlayerFavorite,
                onAddToPlaylist = trackActionsViewModel::open,
                modifier = dismissGestureModifier,
                immersiveLandscape = immersiveLandscape,
            )
        }
    }
    TrackActionsSheetHost(
        uiState = trackActionsUiState,
        viewModel = trackActionsViewModel,
    )
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
private fun PlayerOverlayContent(
    uiState: PlayerUiState,
    playerViewModel: PlayerViewModel,
    playbackPosition: State<Float>,
    onBack: () -> Unit,
    isFavorite: Boolean,
    onToggleFavorite: () -> Unit,
    onAddToPlaylist: (String) -> Unit,
    modifier: Modifier,
    immersiveLandscape: Boolean,
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
        dismissGestureModifier = modifier,
        immersiveLandscape = immersiveLandscape,
    )
}

@Composable
private fun TrackActionsSheetHost(uiState: TrackActionsUiState, viewModel: TrackActionsViewModel) {
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
