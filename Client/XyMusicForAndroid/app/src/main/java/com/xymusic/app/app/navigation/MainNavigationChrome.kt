package com.xymusic.app.app.navigation

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.material3.Icon
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationRailItem
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.State
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.xymusic.app.feature.player.presentation.PlayerMiniBar
import com.xymusic.app.feature.player.presentation.PlayerUiState
import com.xymusic.app.feature.player.presentation.PlayerViewModel
import com.xymusic.app.ui.theme.spacing

internal val MainNavigationBarHeight = 56.5.dp
internal val MainNavigationRailWidth = 72.dp

private val MainNavigationBarDividerHeight = 0.5.dp

private val PRIMARY_DESTINATIONS =
    listOf(
        MainDestination.Home,
        MainDestination.Mine,
    )

@Composable
internal fun PlayerMiniBarRoute(
    uiState: PlayerUiState,
    playerViewModel: PlayerViewModel,
    playbackPosition: State<Float>,
    onOpenPlayer: () -> Unit,
    modifier: Modifier = Modifier,
    compact: Boolean = false,
) {
    if (uiState.player.currentItem == null) return
    PlayerMiniBar(
        uiState = uiState,
        playbackPosition = playbackPosition,
        onOpenPlayer = onOpenPlayer,
        onTogglePlayback = playerViewModel::togglePlayback,
        onNext = playerViewModel::skipToNext,
        compact = compact,
        modifier = modifier,
    )
}

@Composable
internal fun GlassNavigationBar(
    currentDestination: MainDestination?,
    onDestinationSelected: (MainDestination) -> Unit,
) {
    val colorScheme = MaterialTheme.colorScheme

    Column(
        modifier =
        Modifier
            .fillMaxWidth()
            .background(colorScheme.surface.copy(alpha = 0.94f))
            .windowInsetsPadding(WindowInsets.navigationBars),
    ) {
        Box(
            modifier =
            Modifier
                .fillMaxWidth()
                .height(MainNavigationBarDividerHeight)
                .background(colorScheme.outlineVariant.copy(alpha = 0.55f)),
        )
        Row(
            modifier =
            Modifier
                .fillMaxWidth()
                .height(MainNavigationBarHeight - MainNavigationBarDividerHeight)
                .selectableGroup(),
            horizontalArrangement = Arrangement.SpaceEvenly,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            PRIMARY_DESTINATIONS.forEach { destination ->
                val selected = destination == currentDestination
                val label =
                    androidx.compose.ui.res
                        .stringResource(destination.labelRes)
                BottomNavItem(
                    selected = selected,
                    onClick = { onDestinationSelected(destination) },
                    icon = {
                        Icon(
                            imageVector = if (selected) destination.selectedIcon else destination.unselectedIcon,
                            contentDescription = null,
                            modifier = Modifier.size(24.dp),
                        )
                    },
                    label = label,
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
}

@Composable
private fun BottomNavItem(
    selected: Boolean,
    onClick: () -> Unit,
    icon: @Composable () -> Unit,
    label: String,
    modifier: Modifier = Modifier,
) {
    val colorScheme = MaterialTheme.colorScheme
    val contentColor = if (selected) colorScheme.primary else colorScheme.onSurfaceVariant

    Column(
        modifier =
        modifier
            .fillMaxHeight()
            .selectable(
                selected = selected,
                role = Role.Tab,
                onClick = onClick,
            ).semantics(mergeDescendants = true) {},
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        CompositionLocalProvider(LocalContentColor provides contentColor) {
            icon()
            Spacer(modifier = Modifier.height(2.dp))
            Text(
                text = label,
                style =
                MaterialTheme.typography.labelSmall.copy(
                    fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal,
                ),
            )
        }
    }
}

@Composable
internal fun MainNavigationRail(
    currentDestination: MainDestination?,
    onDestinationSelected: (MainDestination) -> Unit,
    modifier: Modifier = Modifier,
) {
    val colorScheme = MaterialTheme.colorScheme
    Surface(
        modifier =
        modifier
            .windowInsetsPadding(
                WindowInsets.safeDrawing.only(WindowInsetsSides.Start),
            )
            .width(MainNavigationRailWidth)
            .fillMaxHeight(),
        color = colorScheme.surface,
    ) {
        Column(
            modifier =
            Modifier
                .fillMaxHeight()
                .windowInsetsPadding(
                    WindowInsets.safeDrawing.only(WindowInsetsSides.Vertical),
                )
                .padding(vertical = MaterialTheme.spacing.large),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Spacer(modifier = Modifier.weight(1f))
            PRIMARY_DESTINATIONS.forEach { destination ->
                val selected = currentDestination == destination
                val label =
                    androidx.compose.ui.res
                        .stringResource(destination.labelRes)
                NavigationRailItem(
                    selected = selected,
                    onClick = { onDestinationSelected(destination) },
                    icon = {
                        Icon(
                            imageVector = if (selected) destination.selectedIcon else destination.unselectedIcon,
                            contentDescription = null,
                            modifier = Modifier.size(24.dp),
                        )
                    },
                    label = { Text(label) },
                    modifier = Modifier.padding(vertical = MaterialTheme.spacing.extraSmall),
                )
            }
            Spacer(modifier = Modifier.weight(1f))
        }
    }
}

internal fun shouldShowMainBottomBar(route: String?): Boolean =
    MainDestination.fromRoute(route)?.let(PRIMARY_DESTINATIONS::contains) == true
