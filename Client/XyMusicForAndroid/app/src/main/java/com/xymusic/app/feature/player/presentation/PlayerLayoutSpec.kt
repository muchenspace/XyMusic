package com.xymusic.app.feature.player.presentation

import androidx.compose.runtime.Immutable
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/** Window breakpoints follow the Material 3 compact, medium, and expanded size classes. */
internal enum class PlayerWindowWidthClass {
    Compact,
    Medium,
    Expanded,
}

internal enum class PlayerWindowHeightClass {
    Compact,
    Medium,
    Expanded,
}

@Immutable
internal data class PlayerLayoutSpec(
    val width: Dp,
    val height: Dp,
    val isLandscape: Boolean,
    val widthClass: PlayerWindowWidthClass,
    val heightClass: PlayerWindowHeightClass,
) {
    val usesWideArtworkLayout: Boolean
        get() = widthClass != PlayerWindowWidthClass.Compact

    val usesLandscapePlayerLayout: Boolean
        get() = isLandscape

    val compactControls: Boolean
        get() = heightClass == PlayerWindowHeightClass.Compact

    val topBarHeight: Dp
        get() = if (compactControls) 72.dp else 88.dp

    val topBarArtworkSize: Dp
        get() = if (compactControls) 48.dp else 56.dp

    val topBarActionSpacing: Dp
        get() = if (compactControls) 4.dp else 10.dp

    val controlsHorizontalPadding: Dp
        get() = if (compactControls) 22.dp else 30.dp

    val skipButtonSize: Dp
        get() = if (compactControls) 58.dp else 68.dp

    val playButtonSize: Dp
        get() = if (compactControls) 70.dp else 82.dp

    val skipIconSize: Dp
        get() = if (compactControls) 35.dp else 42.dp

    val playIconSize: Dp
        get() = if (compactControls) 46.dp else 56.dp

    val bufferingSize: Dp
        get() = if (compactControls) 32.dp else 38.dp

    val controlsTopPadding: Dp
        get() = 8.dp

    val controlsBottomPadding: Dp
        get() = if (compactControls) 4.dp else 10.dp

    val controlsSectionSpacing: Dp
        get() = if (compactControls) 8.dp else 14.dp

    val landscapeTransportHeight: Dp
        get() = if (compactControls) 52.dp else 60.dp

    val landscapeSectionSpacing: Dp
        get() = if (compactControls) 8.dp else 10.dp

    val landscapePlayButtonSize: Dp
        get() = if (compactControls) 52.dp else 60.dp

    val landscapeSecondaryIconSize: Dp
        get() = if (compactControls) 28.dp else 30.dp

    val landscapePlayIconSize: Dp
        get() = if (compactControls) 40.dp else 44.dp

    val landscapeBufferingSize: Dp
        get() = if (compactControls) 30.dp else 32.dp
}

internal fun playerLayoutSpec(width: Dp, height: Dp): PlayerLayoutSpec =
    PlayerLayoutSpec(
        width = width,
        height = height,
        isLandscape = width > height,
        widthClass =
        when {
            width < PLAYER_MEDIUM_WIDTH -> PlayerWindowWidthClass.Compact
            width < PLAYER_EXPANDED_WIDTH -> PlayerWindowWidthClass.Medium
            else -> PlayerWindowWidthClass.Expanded
        },
        heightClass =
        when {
            height < PLAYER_MEDIUM_HEIGHT -> PlayerWindowHeightClass.Compact
            height < PLAYER_EXPANDED_HEIGHT -> PlayerWindowHeightClass.Medium
            else -> PlayerWindowHeightClass.Expanded
        },
    )

/**
 * Text adapts to the lyric pane rather than device identity. Font sizes remain in sp so Android's
 * accessibility font scale is honored; the bounded base size avoids making tablet lyrics huge.
 */
internal data class PlayerLyricMetrics(
    val fontSizeSp: Float,
    val lineHeightSp: Float,
    val horizontalPadding: Dp,
    val verticalPadding: Dp,
    val lineSpacing: Dp,
    val maxContentWidth: Dp,
    val resumeFollowButtonPadding: Dp,
)

internal fun playerLyricMetrics(width: Dp, height: Dp): PlayerLyricMetrics {
    val widthBasedFontSize =
        when {
            width < PLAYER_COMPACT_LYRIC_WIDTH -> 32
            width < PLAYER_MEDIUM_WIDTH -> 36
            width < PLAYER_EXPANDED_WIDTH -> 38
            else -> 40
        }
    val heightAdjustment =
        when {
            height < PLAYER_COMPACT_LYRIC_HEIGHT -> 6
            height < PLAYER_MEDIUM_HEIGHT -> 2
            else -> 0
        }
    val fontSize = (widthBasedFontSize - heightAdjustment).coerceIn(28, 40)
    val widthLimited = width >= PLAYER_MEDIUM_WIDTH

    return PlayerLyricMetrics(
        fontSizeSp = fontSize.toFloat(),
        lineHeightSp = fontSize * PLAYER_LYRIC_LINE_HEIGHT_MULTIPLIER,
        horizontalPadding =
        when {
            width < PLAYER_COMPACT_LYRIC_WIDTH -> 8.dp
            widthLimited -> 20.dp
            else -> 12.dp
        },
        verticalPadding = if (height < PLAYER_COMPACT_LYRIC_HEIGHT) 16.dp else 24.dp,
        lineSpacing = if (fontSize >= 38) 18.dp else 14.dp,
        maxContentWidth =
        when {
            width >= PLAYER_EXPANDED_WIDTH -> PLAYER_EXPANDED_LYRIC_MAX_WIDTH
            width >= PLAYER_MEDIUM_WIDTH -> PLAYER_MEDIUM_LYRIC_MAX_WIDTH
            else -> width
        },
        resumeFollowButtonPadding = if (height < PLAYER_COMPACT_LYRIC_HEIGHT) 8.dp else 12.dp,
    )
}

private const val PLAYER_LYRIC_LINE_HEIGHT_MULTIPLIER = 1.35f
private val PLAYER_COMPACT_LYRIC_WIDTH = 360.dp
private val PLAYER_MEDIUM_WIDTH = 600.dp
private val PLAYER_EXPANDED_WIDTH = 840.dp
private val PLAYER_COMPACT_LYRIC_HEIGHT = 360.dp
private val PLAYER_MEDIUM_HEIGHT = 480.dp
private val PLAYER_EXPANDED_HEIGHT = 900.dp
private val PLAYER_MEDIUM_LYRIC_MAX_WIDTH = 680.dp
private val PLAYER_EXPANDED_LYRIC_MAX_WIDTH = 760.dp
