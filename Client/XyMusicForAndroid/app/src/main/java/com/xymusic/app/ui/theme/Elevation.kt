package com.xymusic.app.ui.theme

import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

@Immutable
data class Elevation(
    val none: Dp = 0.dp,
    val level0: Dp = 0.dp,
    val level1: Dp = 1.dp,
    val level2: Dp = 2.dp,
    val level3: Dp = 4.dp,
    val level4: Dp = 8.dp,
    val level5: Dp = 12.dp,
    val miniBar: Dp = 2.dp,
    val card: Dp = 0.dp,
    val playerArtwork: Dp = 0.dp,
    val modalSheet: Dp = 8.dp,
)

// Pure-monochrome elevation: in dark we use faint gray overlays rather than shadows;
// in light we use soft shadows. Compose shadow rendering is mostly disabled in favor
// of layered surface containers, but these tokens remain for components that need them.
val DarkElevation =
    Elevation(
        none = 0.dp,
        level0 = 0.dp,
        level1 = 0.dp,
        level2 = 0.dp,
        level3 = 0.dp,
        level4 = 0.dp,
        level5 = 0.dp,
        miniBar = 0.dp,
        card = 0.dp,
        playerArtwork = 0.dp,
        modalSheet = 0.dp,
    )

val LightElevation =
    Elevation(
        none = 0.dp,
        level0 = 0.dp,
        level1 = 0.5.dp,
        level2 = 1.dp,
        level3 = 2.dp,
        level4 = 4.dp,
        level5 = 8.dp,
        miniBar = 1.dp,
        card = 1.dp,
        playerArtwork = 0.dp,
        modalSheet = 8.dp,
    )

internal val LocalElevation = staticCompositionLocalOf { DarkElevation }

val MaterialTheme.elevation: Elevation
    @Composable
    @ReadOnlyComposable
    get() = LocalElevation.current
