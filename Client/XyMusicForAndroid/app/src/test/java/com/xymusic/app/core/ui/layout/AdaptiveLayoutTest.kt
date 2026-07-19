package com.xymusic.app.core.ui.layout

import androidx.compose.ui.unit.dp
import com.google.common.truth.Truth.assertThat
import org.junit.Test

class AdaptiveLayoutTest {
    @Test
    fun wideLandscapeRequiresLandscapeAndMinimumWidth() {
        assertThat(isWideLandscape(600.dp, 480.dp)).isTrue()
        assertThat(isWideLandscape(599.dp, 320.dp)).isFalse()
        assertThat(isWideLandscape(600.dp, 600.dp)).isFalse()
        assertThat(isWideLandscape(480.dp, 600.dp)).isFalse()
    }

    @Test
    fun compactLandscapeUsesExclusiveHeightBoundary() {
        assertThat(isCompactLandscape(740.dp, 479.dp)).isTrue()
        assertThat(isCompactLandscape(740.dp, 480.dp)).isFalse()
        assertThat(isCompactLandscape(479.dp, 320.dp)).isFalse()
    }
}
