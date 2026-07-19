package com.xymusic.app.core.ui.layout

import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

internal val WideLandscapeMinWidth = 600.dp
internal val CompactLandscapeMaxHeight = 480.dp

internal fun isWideLandscape(width: Dp, height: Dp): Boolean = width > height && width >= WideLandscapeMinWidth

internal fun isCompactLandscape(width: Dp, height: Dp): Boolean =
    isWideLandscape(width, height) && height < CompactLandscapeMaxHeight
