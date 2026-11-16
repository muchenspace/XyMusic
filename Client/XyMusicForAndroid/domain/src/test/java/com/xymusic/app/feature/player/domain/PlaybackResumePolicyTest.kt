package com.xymusic.app.feature.player.domain

import com.google.common.truth.Truth.assertThat
import org.junit.Test

class PlaybackResumePolicyTest {
    @Test
    fun completedTrackAlwaysRestartsFromZero() {
        assertThat(resolveResumePositionMs(positionMs = 42_000, durationMs = 180_000, completed = true))
            .isEqualTo(0L)
        assertThat(resolveResumePositionMs(positionMs = 180_000, durationMs = 0, completed = true))
            .isEqualTo(0L)
    }

    @Test
    fun positionWithinCompletionToleranceOrPastDurationRestartsFromZero() {
        listOf(179_000L, 179_999L, 180_000L, 185_000L).forEach { positionMs ->
            assertThat(resolveResumePositionMs(positionMs = positionMs, durationMs = 180_000))
                .isEqualTo(0L)
        }
    }

    @Test
    fun midTrackPositionIsPreservedAndNegativeClampedToZero() {
        assertThat(resolveResumePositionMs(positionMs = 178_999L, durationMs = 180_000))
            .isEqualTo(178_999L)
        assertThat(resolveResumePositionMs(positionMs = -500L, durationMs = 180_000))
            .isEqualTo(0L)
        assertThat(resolveResumePositionMs(positionMs = 42_000L, durationMs = 0L))
            .isEqualTo(42_000L)
    }
}
