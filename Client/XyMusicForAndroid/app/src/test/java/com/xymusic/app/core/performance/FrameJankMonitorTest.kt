package com.xymusic.app.core.performance

import com.google.common.truth.Truth.assertThat
import org.junit.Test

class FrameJankMonitorTest {
    @Test
    fun frameAtBudgetIsNotReportedAsSlow() {
        assertThat(isFrameOverBudget(frameDurationNanos = FRAME_BUDGET_NANOS, frameBudgetNanos = FRAME_BUDGET_NANOS))
            .isFalse()
    }

    @Test
    fun frameAboveBudgetIsReportedAsSlow() {
        assertThat(
            isFrameOverBudget(frameDurationNanos = FRAME_BUDGET_NANOS + 1, frameBudgetNanos = FRAME_BUDGET_NANOS),
        )
            .isTrue()
    }

    @Test
    fun frameBelowBudgetIsNotReportedAsSlow() {
        assertThat(
            isFrameOverBudget(frameDurationNanos = FRAME_BUDGET_NANOS - 1, frameBudgetNanos = FRAME_BUDGET_NANOS),
        )
            .isFalse()
    }

    private companion object {
        const val FRAME_BUDGET_NANOS = 16_666_666L
    }
}
