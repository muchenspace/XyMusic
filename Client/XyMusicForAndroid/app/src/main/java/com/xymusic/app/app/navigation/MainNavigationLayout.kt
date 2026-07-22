package com.xymusic.app.app.navigation

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.zIndex
import com.xymusic.app.feature.player.presentation.CompactPlayerMiniBarHeight
import com.xymusic.app.feature.player.presentation.PlayerMiniBarHeight

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
    val placeChromeBehindContent: Boolean,
)

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
    snackbarHostState: SnackbarHostState,
    navigationRail: @Composable () -> Unit,
    bottomNavigation: @Composable () -> Unit,
    miniPlayer: @Composable (Modifier) -> Unit,
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    val showNavigationRail = config.useNavigationRail && chromeState.showMainNavigation
    val showBottomNavigation = !config.useNavigationRail && chromeState.showMainNavigation
    val chromeZIndex =
        if (chromeState.placeChromeBehindContent) ChromeBehindContentZIndex else InteractiveChromeZIndex
    val snackbarBottomPadding =
        (if (chromeState.showMiniPlayer) config.miniPlayerHeight else 0.dp) +
            (if (showBottomNavigation) MainNavigationBarHeight else 0.dp)

    Box(modifier = modifier.fillMaxSize()) {
        if (showNavigationRail) {
            Box(modifier = Modifier.zIndex(chromeZIndex)) {
                navigationRail()
            }
        }
        Column(
            modifier =
            Modifier
                .align(Alignment.BottomCenter)
                .zIndex(chromeZIndex),
        ) {
            if (chromeState.showMiniPlayer) {
                miniPlayer(
                    mainNavigationMiniPlayerModifier(
                        showNavigationRail = showNavigationRail,
                        showBottomNavigation = showBottomNavigation,
                    ),
                )
            }
            if (showBottomNavigation) {
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
                    start = if (showNavigationRail) MainNavigationRailWidth else 0.dp,
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
    visibleRoutes: List<String?>,
): MainNavigationChromeState {
    val foregroundRoute = currentRoute ?: visibleRoutes.lastOrNull() ?: MainDestination.Home.route
    val transitionRoutes = (visibleRoutes + foregroundRoute).distinct()
    val chromeRoutes =
        if (transitionRoutes.any { route -> route == PlayerDestination.NowPlaying.route }) {
            transitionRoutes
        } else {
            listOf(foregroundRoute)
        }
    return MainNavigationChromeState(
        showMainNavigation = chromeRoutes.any(::shouldShowMainBottomBar),
        showMiniPlayer =
        config.hasPlayerItem &&
            chromeRoutes.any { route -> route != null && route != PlayerDestination.NowPlaying.route },
        selectedMainDestination =
        chromeRoutes
            .asReversed()
            .firstNotNullOfOrNull { route -> MainDestination.fromRoute(route) },
        placeChromeBehindContent =
        transitionRoutes.any { route -> route == PlayerDestination.NowPlaying.route },
    )
}

internal fun mainNavigationContentLayout(route: String?): MainNavigationContentLayout = when {
    MainDestination.fromRoute(route) != null -> MainNavigationContentLayout.Primary
    route == PlayerDestination.NowPlaying.route -> MainNavigationContentLayout.FullScreen
    route == PlaylistDestination.Detail.route -> MainNavigationContentLayout.EdgeToEdge
    else -> MainNavigationContentLayout.Secondary
}

@Composable
private fun mainNavigationMiniPlayerModifier(showNavigationRail: Boolean, showBottomNavigation: Boolean): Modifier =
    when {
        showNavigationRail ->
            Modifier
                .windowInsetsPadding(
                    WindowInsets.safeDrawing.only(
                        WindowInsetsSides.Start + WindowInsetsSides.End + WindowInsetsSides.Bottom,
                    ),
                ).padding(start = MainNavigationRailWidth)

        showBottomNavigation ->
            Modifier.windowInsetsPadding(
                WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal),
            )

        else ->
            Modifier.windowInsetsPadding(
                WindowInsets.safeDrawing.only(
                    WindowInsetsSides.Horizontal + WindowInsetsSides.Bottom,
                ),
            )
    }

private const val ChromeBehindContentZIndex = 0f
private const val NavigationContentZIndex = 1f
private const val InteractiveChromeZIndex = 2f
private const val SnackbarZIndex = 3f
