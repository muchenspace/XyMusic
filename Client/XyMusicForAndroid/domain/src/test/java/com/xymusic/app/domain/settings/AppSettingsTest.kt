package com.xymusic.app.domain.settings

import com.google.common.truth.Truth.assertThat
import org.junit.Assert.assertThrows
import org.junit.Test

class AppSettingsTest {
    @Test
    fun cacheLimitWithinBoundsIsAccepted() {
        assertThat(AppSettings(cacheLimitMiB = 128).cacheLimitMiB).isEqualTo(128)
        assertThat(AppSettings(cacheLimitMiB = 4_096).cacheLimitMiB).isEqualTo(4_096)
    }

    @Test
    fun cacheLimitOutOfBoundsIsRejected() {
        assertThrows(IllegalArgumentException::class.java) { AppSettings(cacheLimitMiB = 127) }
        assertThrows(IllegalArgumentException::class.java) { AppSettings(cacheLimitMiB = 4_097) }
    }

    @Test
    fun onlyBaseThemesSupportDynamicColor() {
        assertThat(ThemePreference.SYSTEM.supportsDynamicColor).isTrue()
        assertThat(ThemePreference.PEACH_PINK.supportsDynamicColor).isFalse()
    }
}
