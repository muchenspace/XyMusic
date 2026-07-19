package com.xymusic.app.feature.player.service

import android.os.Bundle
import androidx.media3.session.SessionCommand

internal object PlaybackSessionCommands {
    const val ARG_SLEEP_TIMER_DURATION_MS = "sleep_timer_duration_ms"
    const val EXTRA_SLEEP_TIMER_DEADLINE_ELAPSED_REALTIME_MS =
        "sleep_timer_deadline_elapsed_realtime_ms"
    const val ACTION_CODEC_FALLBACK_APPLIED =
        "com.xymusic.app.player.CODEC_FALLBACK_APPLIED"

    val SET_SLEEP_TIMER =
        SessionCommand(
            "com.xymusic.app.player.SET_SLEEP_TIMER",
            Bundle.EMPTY,
        )
    val GET_SLEEP_TIMER =
        SessionCommand(
            "com.xymusic.app.player.GET_SLEEP_TIMER",
            Bundle.EMPTY,
        )
    val SLEEP_TIMER_CHANGED =
        SessionCommand(
            "com.xymusic.app.player.SLEEP_TIMER_CHANGED",
            Bundle.EMPTY,
        )
    val CODEC_FALLBACK_APPLIED =
        SessionCommand(
            ACTION_CODEC_FALLBACK_APPLIED,
            Bundle.EMPTY,
        )

    fun sleepTimerStateExtras(deadlineElapsedRealtimeMs: Long?): Bundle = Bundle().apply {
        deadlineElapsedRealtimeMs?.let {
            putLong(EXTRA_SLEEP_TIMER_DEADLINE_ELAPSED_REALTIME_MS, it)
        }
    }

    fun sleepTimerDeadline(extras: Bundle): Long? =
        if (extras.containsKey(EXTRA_SLEEP_TIMER_DEADLINE_ELAPSED_REALTIME_MS)) {
            extras
                .getLong(EXTRA_SLEEP_TIMER_DEADLINE_ELAPSED_REALTIME_MS)
                .takeIf { it > 0L }
        } else {
            null
        }
}
