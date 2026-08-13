package com.xymusic.app.feature.player.presentation

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp
import com.xymusic.app.core.ui.component.ArtworkAmbientPalette
import com.xymusic.app.ui.theme.XyColors

internal data class PlayerBackdropColors(val start: Color, val center: Color, val highlight: Color, val end: Color)

/**
 * Keeps the selected theme in every stop while letting artwork drive the
 * visible chromatic shift through the whole gradient.
 */
internal fun resolvePlayerBackdropColors(
    themeColors: XyColors,
    ambientPalette: ArtworkAmbientPalette,
): PlayerBackdropColors = PlayerBackdropColors(
    start = lerp(themeColors.nowPlayingBg, ambientPalette.primary, 0.56f),
    center = lerp(themeColors.gradientStart, ambientPalette.secondary, 0.48f),
    highlight = lerp(themeColors.gradientEnd, ambientPalette.highlight, 0.40f),
    end = lerp(themeColors.gradientEnd, ambientPalette.secondary, 0.22f),
)
