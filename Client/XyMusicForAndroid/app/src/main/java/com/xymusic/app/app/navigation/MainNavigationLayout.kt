package com.xymusic.app.app.navigation

import androidx.compose.animation.core.animateDp
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.tween
import androidx.compose.animation.core.updateTransition
import androidx.compose.foundation.layout.Box
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
    content: @Composable () -> Unit,
) {
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
    val miniPlayerBottomOffset by
        chromeTransition.animateDp(
            transitionSpec = {
                tween(
                    durationMillis = chromeTransitionDuration(initialState, targetState),
                    easing = XyMotion.NavigationEasing,
                )
            },
            label = "miniPlayerBottomOffset",
        ) { state ->
            if (!config.useNavigationRail && state.showMainNavigation) MainNavigationBarHeight else 0.dp
        }
    val miniPlayerStartPadding by
        chromeTransition.animateDp(
            transitionSpec = {
                tween(
                    durationMillis = chromeTransitionDuration(initialState, targetState),
                    easing = XyMotion.NavigationEasing,
                )
            },
            label = "miniPlayerStartPadding",
        ) { state ->
            if (config.useNavigationRail && state.showMainNavigation) MainNavigationRailWidth else 0.dp
        }
    val snackbarBottomPadding by
        chromeTransition.animateDp(
            transitionSpec = {
                tween(
                    durationMillis = chromeTransitionDuration(initialState, targetState),
                    easing = XyMotion.NavigationEasing,
                )
            },
            label = "snackbarBottomPadding",
        ) { state ->
            (if (state.showMiniPlayer) config.miniPlayerHeight else 0.dp) +
                if (!config.useNavigationRail && state.showMainNavigation) MainNavigationBarHeight else 0.dp
        }
    val snackbarStartPadding by
        chromeTransition.animateDp(
            transitionSpec = {
                tween(
                    durationMillis = chromeTransitionDuration(initialState, targetState),
                    easing = XyMotion.NavigationEasing,
                )
            },
            label = "snackbarStartPadding",
        ) { state ->
            if (config.useNavigationRail && state.showMainNavigation) MainNavigationRailWidth else 0.dp
        }
    val showNavigationRail =
        config.useNavigationRail &&
            (chromeTransition.currentState.showMainNavigation || chromeTransition.targetState.showMainNavigation)
    val showBottomNavigation =
        !config.useNavigationRail &&
            (chromeTransition.currentState.showMainNavigation || chromeTransition.targetState.showMainNavigation)
    val showMiniPlayer =
        chromeTransition.currentState.showMiniPlayer || chromeTransition.targetState.showMiniPlayer
    val chromeIsExitingForPlayer =
        playerEntryStillVisible &&
            chromeTransition.currentState.hasVisibleChrome &&
            !chromeTransition.targetState.hasVisibleChrome
    val chromeZIndex =
        if (playerEntryStillVisible && !chromeIsExitingForPlayer) {
            ChromeBehindContentZIndex
        } else {
            InteractiveChromeZIndex
        }

    Box(modifier = modifier.fillMaxSize()) {
        if (showNavigationRail) {
            Box(
                modifier =
                Modifier
                    .zIndex(chromeZIndex)
                    .graphicsLayer { alpha = navigationAlpha },
            ) {
                navigationRail()
            }
        }
        if (showMiniPlayer) {
            Box(
                modifier =
                Modifier
                    .align(Alignment.BottomCenter)
                    .offset(y = -miniPlayerBottomOffset)
                    .zIndex(chromeZIndex)
                    .graphicsLayer { alpha = miniPlayerAlpha },
            ) {
                miniPlayer(mainNavigationMiniPlayerModifier(miniPlayerStartPadding))
            }
        }
        if (showBottomNavigation) {
            Box(
                modifier =
                Modifier
                    .align(Alignment.BottomCenter)
                    .zIndex(chromeZIndex)
                    .graphicsLayer { alpha = navigationAlpha },
            ) {
                bottomNavigation()
            }
        }
        Box(
            modifier =
            Modifier
                .fillMaxSize()
                .zIndex(NavigationContentZIndex),
        ) {
            content()
        }
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
                    start = snackbarStartPadding,
                    bottom = snackbarBottomPadding,
                ).zIndex(SnackbarZIndex),
        )
    }
}

@Composable
internal fun MainNavigationRouteLayout(
    layout: MainNavigationContentLayout,
    config: MainNavigationLayoutConfig,
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    val layoutModifier =
        when (layout) {
            MainNavigationContentLayout.Primary ->
                Modifier
                    .windowInsetsPadding(WindowInsets.safeDrawing)
                    .padding(
                        start = if (config.useNavigationRail) MainNavigationRailWidth else 0.dp,
                        bottom =
                        config.miniPlayerHeight +
                            if (config.useNavigationRail) 0.dp else MainNavigationBarHeight,
                    )

            MainNavigationContentLayout.Secondary ->
                Modifier
                    .windowInsetsPadding(WindowInsets.safeDrawing)
                    .padding(bottom = config.miniPlayerHeight)

            MainNavigationContentLayout.EdgeToEdge ->
                if (config.hasPlayerItem) {
                    Modifier
                        .windowInsetsPadding(
                            WindowInsets.safeDrawing.only(WindowInsetsSides.Bottom),
                        ).padding(bottom = config.miniPlayerHeight)
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
private fun mainNavigationMiniPlayerModifier(startPadding: Dp): Modifier = Modifier
    .windowInsetsPadding(
        WindowInsets.safeDrawing.only(
            WindowInsetsSides.Horizontal + WindowInsetsSides.Bottom,
        ),
    ).padding(start = startPadding)

private fun chromeTransitionDuration(
    initialState: MainNavigationChromeState,
    targetState: MainNavigationChromeState,
): Int = if (initialState.isPlayerDestination || targetState.isPlayerDestination) {
    XyMotion.Emphasized
} else {
    XyMotion.Standard
}

private const val ChromeBehindContentZIndex = 0f
private const val NavigationContentZIndex = 1f
private const val InteractiveChromeZIndex = 2f
private const val SnackbarZIndex = 3f
