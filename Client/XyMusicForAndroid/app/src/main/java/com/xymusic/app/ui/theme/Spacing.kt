package com.xymusic.app.ui.theme

import androidx.compose.runtime.Immutable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

@Immutable
data class Spacing(
    val extraSmall: Dp = 4.dp,
    val small: Dp = 8.dp,
    val compact: Dp = 12.dp,
    val medium: Dp = 16.dp,
    val semiLarge: Dp = 20.dp,
    val large: Dp = 24.dp,
    val extraLarge: Dp = 32.dp,
    val huge: Dp = 48.dp,
    val massive: Dp = 64.dp,
    val contentPadding: Dp = 20.dp,
    val cardPadding: Dp = 16.dp,
    val listRowPadding: Dp = 12.dp,
    val iconButtonSize: Dp = 44.dp,
    val touchTarget: Dp = 48.dp,
    val artworkSmall: Dp = 48.dp,
    val artworkMedium: Dp = 56.dp,
    val artworkList: Dp = 44.dp,
)

internal val LocalSpacing = staticCompositionLocalOf { Spacing() }
