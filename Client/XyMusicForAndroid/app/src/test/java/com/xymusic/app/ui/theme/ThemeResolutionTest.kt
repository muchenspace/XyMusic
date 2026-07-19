package com.xymusic.app.ui.theme

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.preferences.ThemePreference
import org.junit.Test

class ThemeResolutionTest {
    @Test
    fun systemThemeFollowsSystemDarkMode() {
        assertThat(ThemePreference.SYSTEM.resolveDarkTheme(false)).isFalse()
        assertThat(ThemePreference.SYSTEM.resolveDarkTheme(true)).isTrue()
    }

    @Test
    fun fixedAndColorThemesIgnoreSystemDarkMode() {
        assertThat(ThemePreference.LIGHT.resolveDarkTheme(true)).isFalse()
        assertThat(ThemePreference.DARK.resolveDarkTheme(false)).isTrue()
        assertThat(ThemePreference.PEACH_PINK.resolveDarkTheme(true)).isFalse()
        assertThat(ThemePreference.OCEAN_BLUE.resolveDarkTheme(true)).isFalse()
        assertThat(ThemePreference.TWILIGHT_PURPLE.resolveDarkTheme(true)).isFalse()
    }

    @Test
    fun staticColorSchemesMapEveryTheme() {
        assertThat(resolveStaticColorScheme(ThemePreference.SYSTEM, false))
            .isSameInstanceAs(LightColors)
        assertThat(resolveStaticColorScheme(ThemePreference.SYSTEM, true))
            .isSameInstanceAs(DarkColors)
        assertThat(resolveStaticColorScheme(ThemePreference.LIGHT, false))
            .isSameInstanceAs(LightColors)
        assertThat(resolveStaticColorScheme(ThemePreference.DARK, true))
            .isSameInstanceAs(DarkColors)
        assertThat(resolveStaticColorScheme(ThemePreference.PEACH_PINK, false))
            .isSameInstanceAs(PeachPinkColors)
        assertThat(resolveStaticColorScheme(ThemePreference.OCEAN_BLUE, false))
            .isSameInstanceAs(OceanBlueColors)
        assertThat(resolveStaticColorScheme(ThemePreference.TWILIGHT_PURPLE, false))
            .isSameInstanceAs(TwilightPurpleColors)
    }

    @Test
    fun dynamicColorEligibilityExcludesFixedColorThemes() {
        assertThat(ThemePreference.SYSTEM.supportsDynamicColor).isTrue()
        assertThat(ThemePreference.LIGHT.supportsDynamicColor).isTrue()
        assertThat(ThemePreference.DARK.supportsDynamicColor).isTrue()
        assertThat(ThemePreference.PEACH_PINK.supportsDynamicColor).isFalse()
        assertThat(ThemePreference.OCEAN_BLUE.supportsDynamicColor).isFalse()
        assertThat(ThemePreference.TWILIGHT_PURPLE.supportsDynamicColor).isFalse()
    }
}
