package com.xymusic.app.feature.player.presentation

import androidx.compose.ui.unit.dp
import com.google.common.truth.Truth.assertThat
import org.junit.Test

class PlayerLayoutSpecTest {
    @Test
    fun windowClassesFollowCompactMediumAndExpandedWidthAndHeightBreakpoints() {
        assertThat(playerLayoutSpec(599.dp, 479.dp))
            .isEqualTo(
                PlayerLayoutSpec(
                    width = 599.dp,
                    height = 479.dp,
                    isLandscape = true,
                    widthClass = PlayerWindowWidthClass.Compact,
                    heightClass = PlayerWindowHeightClass.Compact,
                ),
            )
        assertThat(playerLayoutSpec(600.dp, 480.dp))
            .isEqualTo(
                PlayerLayoutSpec(
                    width = 600.dp,
                    height = 480.dp,
                    isLandscape = true,
                    widthClass = PlayerWindowWidthClass.Medium,
                    heightClass = PlayerWindowHeightClass.Medium,
                ),
            )
        assertThat(playerLayoutSpec(840.dp, 900.dp))
            .isEqualTo(
                PlayerLayoutSpec(
                    width = 840.dp,
                    height = 900.dp,
                    isLandscape = false,
                    widthClass = PlayerWindowWidthClass.Expanded,
                    heightClass = PlayerWindowHeightClass.Expanded,
                ),
            )
    }

    @Test
    fun widthAndHeightIndependentlyChooseArtworkAndControlLayouts() {
        val shortTabletWindow = playerLayoutSpec(width = 700.dp, height = 420.dp)

        assertThat(shortTabletWindow.usesWideArtworkLayout).isTrue()
        assertThat(shortTabletWindow.usesLandscapePlayerLayout).isTrue()
        assertThat(shortTabletWindow.compactControls).isTrue()
        assertThat(shortTabletWindow.skipButtonSize).isAtLeast(48.dp)
        assertThat(shortTabletWindow.playButtonSize).isAtLeast(48.dp)
    }

    @Test
    fun narrowLandscapeWindowKeepsLandscapeStructureAndCompactControls() {
        val narrowWindow = playerLayoutSpec(width = 580.dp, height = 320.dp)

        assertThat(narrowWindow.usesWideArtworkLayout).isFalse()
        assertThat(narrowWindow.usesLandscapePlayerLayout).isTrue()
        assertThat(narrowWindow.compactControls).isTrue()
    }

    @Test
    fun lyricMetricsFitShortPhoneAndCapTabletReadingWidth() {
        val shortPhone = playerLyricMetrics(width = 360.dp, height = 320.dp)
        val portraitTablet = playerLyricMetrics(width = 700.dp, height = 900.dp)
        val expandedTablet = playerLyricMetrics(width = 1_000.dp, height = 1_000.dp)

        assertThat(shortPhone.fontSizeSp).isEqualTo(30f)
        assertThat(shortPhone.lineHeightSp).isEqualTo(40.5f)
        assertThat(portraitTablet.fontSizeSp).isEqualTo(38f)
        assertThat(portraitTablet.maxContentWidth).isEqualTo(680.dp)
        assertThat(expandedTablet.fontSizeSp).isEqualTo(40f)
        assertThat(expandedTablet.maxContentWidth).isEqualTo(760.dp)
    }
}
