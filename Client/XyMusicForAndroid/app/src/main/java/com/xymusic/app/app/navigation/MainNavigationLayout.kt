package com.xymusic.app.app.navigation

import androidx.compose.animation.core.animateDp
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.tween
import androidx.compose.animation.core.updateTransition
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.zIndex
import com.xymusic.app.feature.player.presentation.CompactPlayerMiniBarHeight
import com.xymusic.app.feature.player.presentation.PlayerMiniBarHeight
import com.xymusic.app.ui.theme.XyMotion

@Immutable
internal data class MainNavigationLayoutConfig(
    val useNavigationRail: Boolean,
    val compactPlayerBar: Boolean,
    val hasPlayerItem: Boolean,
) {
    val miniPlayerHeight: Dp
        get() = if (hasPlayerItem) {
            if (compactPlayerBar) CompactPlayerMiniBarHeight else PlayerMiniBarHeight
        } else {
            0.dp
        }
}

@Immutable
internal data class MainNavigationChromeState(
    val showMainNavigation: Boolean,
    val showMiniPlayer: Boolean,
    val selectedMainDestination: MainDestination?,
    val isPlayerDestination: Boolean,
)

@Immutable
internal data class MainNavigationChromeInsets(
    val primaryStartPadding: Dp,
    val primaryBottomPadding: Dp,
    val miniPlayerBottomPadding: Dp,
)

private val MainNavigationChromeState.hasVisibleChrome: Boolean
    get() = showMainNavigation || showMiniPlayer

internal enum class MainNavigationContentLayout {
    Primary,
    Secondary,
    EdgeToEdge,
    FullScreen,
}

@Composable
internal fun MainNavigationLayout(
    config: MainNavigationLayoutConfig,
    chromeState: MainNavigationChromeState,
    playerEntryStillVisible: Boolean,
    snackbarHostState: SnackbarHostState,
    navigationRail: @Composable () -> Unit,
    bottomNavigation: @Composable () -> Unit,
    miniPlayer: @Composable (Modifier) -> Unit,
    modifier: Modifier = Modifier,
    content: @Composable (MainNavigationChromeInsets) -> Unit,
) {
    val chromeMotion = rememberMainNavigationChromeMotion(config, chromeState)
    val chromeZIndex =
        chromeZIndex(
            playerEntryStillVisible = playerEntryStillVisible,
            chromeIsExitingForPlayer = chromeMotion.isExitingForPlayer,
        )

    Box(modifier = modifier.fillMaxSize()) {
        MainNavigationChrome(
            motion = chromeMotion,
            chromeZIndex = chromeZIndex,
            navigationRail = navigationRail,
            bottomNavigation = bottomNavigation,
            miniPlayer = miniPlayer,
        )
        Box(
            modifier =
            Modifier
                .fillMaxSize()
                .zIndex(NAVIGATION_CONTENT_Z_INDEX),
        ) {
            content(chromeMotion.insets)
        }
        MainNavigationSnackbar(
            snackbarHostState = snackbarHostState,
            insets = chromeMotion.insets,
        )
    }
}

@Composable
private fun rememberMainNavigationChromeMotion(
    config: MainNavigationLayoutConfig,
    chromeState: MainNavigationChromeState,
): MainNavigationChromeMotion {
    val chromeTransition = updateTransition(targetState = chromeState, label = "mainNavigationChrome")
    val navigationAlpha by
        chromeTransition.animateFloat(
            transitionSpec = {
                tween(
                    durationMillis = chromeTransitionDuration(initialState, targetState),
                    easing = XyMotion.NavigationEasing,
                )
            },
            label = "mainNavigationAlpha",
        ) { state ->
            if (state.showMainNavigation) 1f else 0f
        }
    val miniPlayerAlpha by
        chromeTransition.animateFloat(
            transitionSpec = {
                tween(
                    durationMillis = chromeTransitionDuration(initialState, targetState),
                    easing = XyMotion.NavigationEasing,
                )
            },
            label = "miniPlayerAlpha",
        ) { state ->
            if (state.showMiniPlayer) 1f else 0f
        }
    val primaryNavigationBottomPadding by
        chromeTransition.animateDp(
            transitionSpec = {
                tween(
                    durationMillis = chromeTransitionDuration(initialState, targetState),
                    easing = XyMotion.NavigationEasing,
                )
            },
            label = "primaryNavigationBottomPadding",
        ) { state ->
            state.primaryNavigationBottomPadding(config)
        }
    val primaryStartPadding by
        chromeTransition.animateDp(
            transitionSpec = {
                tween(
                    durationMillis = chromeTransitionDuration(initialState, targetState),
                    easing = XyMotion.NavigationEasing,
                )
            },
            label = "primaryStartPadding",
        ) { state ->
            state.primaryStartPadding(config)
        }
    val miniPlayerBottomPadding by
        chromeTransition.animateDp(
            transitionSpec = {
                tween(
                    durationMillis = chromeTransitionDuration(initialState, targetState),
                    easing = XyMotion.NavigationEasing,
                )
            },
            label = "miniPlayerBottomPadding",
        ) { state ->
            state.miniPlayerBottomPadding(config)
        }
    val visibility =
        mainNavigationChromeVisibility(
            config = config,
            currentState = chromeTransition.currentState,
            targetState = chromeTransition.targetState,
        )
    return MainNavigationChromeMotion(
        navigationAlpha = navigationAlpha,
        miniPlayerAlpha = miniPlayerAlpha,
        miniPlayerBottomOffset = primaryNavigationBottomPadding,
        miniPlayerStartPadding = primaryStartPadding,
        insets =
        MainNavigationChromeInsets(
            primaryStartPadding = primaryStartPadding,
            primaryBottomPadding = miniPlayerBottomPadding + primaryNavigationBottomPadding,
            miniPlayerBottomPadding = miniPlayerBottomPadding,
        ),
        showNavigationRail = visibility.showNavigationRail,
        showBottomNavigation = visibility.showBottomNavigation,
        showMiniPlayer = visibility.showMiniPlayer,
        isExitingForPlayer = visibility.isExitingForPlayer,
    )
}

@Composable
private fun BoxScope.MainNavigationChrome(
    motion: MainNavigationChromeMotion,
    chromeZIndex: Float,
    navigationRail: @Composable () -> Unit,
    bottomNavigation: @Composable () -> Unit,
    miniPlayer: @Composable (Modifier) -> Unit,
) {
    if (motion.showNavigationRail) {
        Box(
            modifier =
            Modifier
                .zIndex(chromeZIndex)
                .graphicsLayer { alpha = motion.navigationAlpha },
        ) {
            navigationRail()
        }
    }
    if (motion.showMiniPlayer) {
        Box(
            modifier =
            Modifier
                .align(Alignment.BottomCenter)
                .offset(y = -motion.miniPlayerBottomOffset)
                .zIndex(chromeZIndex)
                .graphicsLayer { alpha = motion.miniPlayerAlpha },
        ) {
            miniPlayer(Modifier.mainNavigationMiniPlayerModifier(motion.miniPlayerStartPadding))
        }
    }
    if (motion.showBottomNavigation) {
        Box(
            modifier =
            Modifier
                .align(Alignment.BottomCenter)
                .zIndex(chromeZIndex)
                .graphicsLayer { alpha = motion.navigationAlpha },
        ) {
            bottomNavigation()
        }
    }
}

@Composable
private fun BoxScope.MainNavigationSnackbar(
    snackbarHostState: SnackbarHostState,
    insets: MainNavigationChromeInsets,
) {
    SnackbarHost(
        hostState = snackbarHostState,
        modifier =
        Modifier
            .align(Alignment.BottomCenter)
            .windowInsetsPadding(
                WindowInsets.safeDrawing.only(
                    WindowInsetsSides.Horizontal + WindowInsetsSides.Bottom,
                ),
            ).padding(
                start = insets.primaryStartPadding,
                bottom = insets.primaryBottomPadding,
            ).zIndex(SNACKBAR_Z_INDEX),
    )
}

@Composable
internal fun MainNavigationRouteLayout(
    layout: MainNavigationContentLayout,
    config: MainNavigationLayoutConfig,
    chromeInsets: MainNavigationChromeInsets,
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    val layoutModifier =
        when (layout) {
            MainNavigationContentLayout.Primary ->
                Modifier
                    .windowInsetsPadding(WindowInsets.safeDrawing)
                    .padding(
                        start = chromeInsets.primaryStartPadding,
                        bottom = chromeInsets.primaryBottomPadding,
                    )

            MainNavigationContentLayout.Secondary ->
                Modifier
                    .windowInsetsPadding(WindowInsets.safeDrawing)
                    .padding(bottom = chromeInsets.miniPlayerBottomPadding)

            MainNavigationContentLayout.EdgeToEdge ->
                if (config.hasPlayerItem) {
                    Modifier
                        .windowInsetsPadding(
                            WindowInsets.safeDrawing.only(WindowInsetsSides.Bottom),
                        ).padding(bottom = chromeInsets.miniPlayerBottomPadding)
                } else {
                    Modifier
                }

            MainNavigationContentLayout.FullScreen -> Modifier
        }

    Box(
        modifier =
        modifier
            .fillMaxSize()
            .then(layoutModifier),
    ) {
        content()
    }
}

internal fun mainNavigationChromeState(
    config: MainNavigationLayoutConfig,
    currentRoute: String?,
    lastSelectedMainDestination: MainDestination? = null,
): MainNavigationChromeState {
    val foregroundRoute = currentRoute ?: MainDestination.Home.route
    val isPlayerDestination = foregroundRoute == PlayerDestination.NowPlaying.route
    return MainNavigationChromeState(
        showMainNavigation = !isPlayerDestination && shouldShowMainBottomBar(foregroundRoute),
        showMiniPlayer = config.hasPlayerItem && !isPlayerDestination,
        selectedMainDestination =
        MainDestination.fromRoute(foregroundRoute) ?: lastSelectedMainDestination,
        isPlayerDestination = isPlayerDestination,
    )
}

internal fun mainNavigationContentLayout(route: String?): MainNavigationContentLayout = when {
    MainDestination.fromRoute(route) != null -> MainNavigationContentLayout.Primary
    route == PlayerDestination.NowPlaying.route -> MainNavigationContentLayout.FullScreen
    route == PlaylistDestination.Detail.route -> MainNavigationContentLayout.EdgeToEdge
    else -> MainNavigationContentLayout.Secondary
}

@Composable
private fun Modifier.mainNavigationMiniPlayerModifier(startPadding: Dp): Modifier =
    this.windowInsetsPadding(
        WindowInsets.safeDrawing.only(
            WindowInsetsSides.Horizontal + WindowInsetsSides.Bottom,
        ),
    ).padding(start = startPadding)

private fun chromeTransitionDuration(
    initialState: MainNavigationChromeState,
    targetState: MainNavigationChromeState,
): Int =
    when {
        targetState.isPlayerDestination -> XyMotion.Emphasized
        initialState.isPlayerDestination -> XyMotion.Slow
        else -> XyMotion.Standard
    }

private fun chromeZIndex(playerEntryStillVisible: Boolean, chromeIsExitingForPlayer: Boolean): Float =
    if (playerEntryStillVisible && !chromeIsExitingForPlayer) {
        CHROME_BEHIND_CONTENT_Z_INDEX
    } else {
        INTERACTIVE_CHROME_Z_INDEX
    }

private fun MainNavigationChromeState.primaryNavigationBottomPadding(
    config: MainNavigationLayoutConfig,
): Dp = if (!config.useNavigationRail && showMainNavigation) MainNavigationBarHeight else 0.dp

private fun MainNavigationChromeState.primaryStartPadding(config: MainNavigationLayoutConfig): Dp =
    if (config.useNavigationRail && showMainNavigation) MainNavigationRailWidth else 0.dp

private fun MainNavigationChromeState.miniPlayerBottomPadding(config: MainNavigationLayoutConfig): Dp =
    if (showMiniPlayer) config.miniPlayerHeight else 0.dp

private fun mainNavigationChromeVisibility(
    config: MainNavigationLayoutConfig,
    currentState: MainNavigationChromeState,
    targetState: MainNavigationChromeState,
): MainNavigationChromeVisibility {
    val mainNavigationVisible = currentState.showMainNavigation || targetState.showMainNavigation
    return MainNavigationChromeVisibility(
        showNavigationRail = config.useNavigationRail && mainNavigationVisible,
        showBottomNavigation = !config.useNavigationRail && mainNavigationVisible,
        showMiniPlayer = currentState.showMiniPlayer || targetState.showMiniPlayer,
        isExitingForPlayer = currentState.hasVisibleChrome && !targetState.hasVisibleChrome,
    )
}

private data class MainNavigationChromeMotion(
    val navigationAlpha: Float,
    val miniPlayerAlpha: Float,
    val miniPlayerBottomOffset: Dp,
    val miniPlayerStartPadding: Dp,
    val insets: MainNavigationChromeInsets,
    val showNavigationRail: Boolean,
    val showBottomNavigation: Boolean,
    val showMiniPlayer: Boolean,
    val isExitingForPlayer: Boolean,
)

private data class MainNavigationChromeVisibility(
    val showNavigationRail: Boolean,
    val showBottomNavigation: Boolean,
    val showMiniPlayer: Boolean,
    val isExitingForPlayer: Boolean,
)

private const val CHROME_BEHIND_CONTENT_Z_INDEX = 0f
private const val NAVIGATION_CONTENT_Z_INDEX = 1f
private const val INTERACTIVE_CHROME_Z_INDEX = 2f
private const val SNACKBAR_Z_INDEX = 3f
