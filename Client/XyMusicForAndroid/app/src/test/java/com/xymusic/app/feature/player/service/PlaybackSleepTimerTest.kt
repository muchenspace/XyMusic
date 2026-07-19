package com.xymusic.app.feature.player.service

import com.google.common.truth.Truth.assertThat
import org.junit.Assert.assertThrows
import org.junit.Test

class PlaybackSleepTimerTest {
    @Test
    fun startTracksDeadlineAndRemainingTimeFromElapsedRealtime() {
        val state =
            PlaybackSleepTimerState().start(
                durationMs = 60_000L,
                nowElapsedRealtimeMs = 10_000L,
            )

        assertThat(state.deadlineElapsedRealtimeMs).isEqualTo(70_000L)
        assertThat(state.remainingMs(25_000L)).isEqualTo(45_000L)
        assertThat(state.isExpired(69_999L)).isFalse()
        assertThat(state.isExpired(70_000L)).isTrue()
        assertThat(state.remainingMs(80_000L)).isEqualTo(0L)
    }

    @Test
    fun restartAndCancelReplacePreviousTimerState() {
        val initial = PlaybackSleepTimerState().start(15_000L, 1_000L)
        val restarted = initial.start(30_000L, 5_000L)
        val cancelled = restarted.cancel()

        assertThat(initial.deadlineElapsedRealtimeMs).isEqualTo(16_000L)
        assertThat(restarted.deadlineElapsedRealtimeMs).isEqualTo(35_000L)
        assertThat(cancelled.deadlineElapsedRealtimeMs).isNull()
        assertThat(cancelled.remainingMs(10_000L)).isNull()
        assertThat(cancelled.isExpired(10_000L)).isFalse()
    }

    @Test
    fun startRejectsNonPositiveAndExcessiveDurations() {
        assertThrows(IllegalArgumentException::class.java) {
            PlaybackSleepTimerState().start(0L, 1_000L)
        }
        assertThrows(IllegalArgumentException::class.java) {
            PlaybackSleepTimerState().start(MAX_SLEEP_TIMER_DURATION_MS + 1L, 1_000L)
        }
    }
}
