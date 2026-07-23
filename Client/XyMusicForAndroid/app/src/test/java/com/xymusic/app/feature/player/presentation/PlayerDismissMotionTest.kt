package com.xymusic.app.feature.player.presentation

import com.google.common.truth.Truth.assertThat
import org.junit.Test

class PlayerDismissMotionTest {
    @Test
    fun shortSlowReleaseRestoresThePlayer() {
        assertThat(
            resolvePlayerDismissTarget(
                offsetPx = 179f,
                releaseVelocityPxPerSecond = 999f,
                distanceThresholdPx = 180f,
                velocityThresholdPxPerSecond = 1_000f,
            ),
        ).isEqualTo(PlayerDismissTarget.Restore)
    }

    @Test
    fun releasePastTheDistanceThresholdDismissesThePlayer() {
        assertThat(
            resolvePlayerDismissTarget(
                offsetPx = 180f,
                releaseVelocityPxPerSecond = 0f,
                distanceThresholdPx = 180f,
                velocityThresholdPxPerSecond = 1_000f,
            ),
        ).isEqualTo(PlayerDismissTarget.Dismiss)
    }

    @Test
    fun shortFastDownwardReleaseDismissesThePlayer() {
        assertThat(
            resolvePlayerDismissTarget(
                offsetPx = 24f,
                releaseVelocityPxPerSecond = 1_001f,
                distanceThresholdPx = 180f,
                velocityThresholdPxPerSecond = 1_000f,
            ),
        ).isEqualTo(PlayerDismissTarget.Dismiss)
    }

    @Test
    fun fastUpwardReleaseRestoresEvenPastTheDistanceThreshold() {
        assertThat(
            resolvePlayerDismissTarget(
                offsetPx = 300f,
                releaseVelocityPxPerSecond = -1_001f,
                distanceThresholdPx = 180f,
                velocityThresholdPxPerSecond = 1_000f,
            ),
        ).isEqualTo(PlayerDismissTarget.Restore)
    }
}
