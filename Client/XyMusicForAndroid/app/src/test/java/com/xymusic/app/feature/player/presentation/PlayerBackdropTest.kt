package com.xymusic.app.feature.player.presentation

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.ui.component.ArtworkAmbientPalette
import com.xymusic.app.ui.theme.DarkXyColors
import com.xymusic.app.ui.theme.LightXyColors
import com.xymusic.app.ui.theme.OceanBlueXyColors
import com.xymusic.app.ui.theme.PeachPinkXyColors
import com.xymusic.app.ui.theme.TwilightPurpleXyColors
import org.junit.Test

class PlayerBackdropTest {
    private val artworkPalette =
        ArtworkAmbientPalette(
            primary = Color(0xFF9A3E67),
            secondary = Color(0xFF287A84),
            highlight = Color(0xFFF0C66A),
        )

    @Test
    fun artworkColorsAreBlendedIntoThemeGradientStops() {
        val backdrop = resolvePlayerBackdropColors(LightXyColors, artworkPalette)

        assertThat(backdrop.start)
            .isEqualTo(lerp(LightXyColors.nowPlayingBg, artworkPalette.primary, 0.56f))
        assertThat(backdrop.center)
            .isEqualTo(lerp(LightXyColors.gradientStart, artworkPalette.secondary, 0.48f))
        assertThat(backdrop.highlight)
            .isEqualTo(lerp(LightXyColors.gradientEnd, artworkPalette.highlight, 0.40f))
    }

    @Test
    fun gradientEndStillUsesTheSelectedThemeAsItsBase() {
        listOf(
            DarkXyColors,
            LightXyColors,
            PeachPinkXyColors,
            OceanBlueXyColors,
            TwilightPurpleXyColors,
        ).forEach { themeColors ->
            assertThat(resolvePlayerBackdropColors(themeColors, artworkPalette).end)
                .isEqualTo(lerp(themeColors.gradientEnd, artworkPalette.secondary, 0.22f))
        }
    }
}
