package com.xymusic.app.feature.player.service

internal const val MAX_SLEEP_TIMER_DURATION_MS = 24L * 60L * 60L * 1_000L

internal data class PlaybackSleepTimerState(val deadlineElapsedRealtimeMs: Long? = null) {
    fun start(durationMs: Long, nowElapsedRealtimeMs: Long): PlaybackSleepTimerState {
        require(durationMs in 1L..MAX_SLEEP_TIMER_DURATION_MS) {
            "Sleep timer duration is out of range"
        }
        require(nowElapsedRealtimeMs >= 0L) { "Elapsed realtime cannot be negative" }
        require(durationMs <= Long.MAX_VALUE - nowElapsedRealtimeMs) {
            "Sleep timer deadline overflow"
        }
        return PlaybackSleepTimerState(nowElapsedRealtimeMs + durationMs)
    }

    fun cancel(): PlaybackSleepTimerState = PlaybackSleepTimerState()

    fun remainingMs(nowElapsedRealtimeMs: Long): Long? = deadlineElapsedRealtimeMs?.let {
        (it - nowElapsedRealtimeMs).coerceAtLeast(0L)
    }

    fun isExpired(nowElapsedRealtimeMs: Long): Boolean =
        deadlineElapsedRealtimeMs != null && remainingMs(nowElapsedRealtimeMs) == 0L
}
