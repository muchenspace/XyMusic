package com.xymusic.app.feature.player.service

import android.os.SystemClock
import androidx.media3.common.Player
import androidx.media3.session.MediaSession
import androidx.media3.session.SessionResult
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

internal class PlaybackSleepTimerController(
    private val serviceScope: CoroutineScope,
    private val player: Player,
    private val mediaSession: () -> MediaSession,
) {
    private var timerJob: Job? = null
    private var state = PlaybackSleepTimerState()

    fun setTimer(durationMs: Long?) {
        timerJob?.cancel()
        timerJob = null
        state =
            if (durationMs == null) {
                state.cancel()
            } else {
                state.start(durationMs, SystemClock.elapsedRealtime())
            }
        broadcastState()
        val expectedDeadline = state.deadlineElapsedRealtimeMs ?: return
        timerJob =
            serviceScope.launch {
                while (isActive) {
                    val remainingMs =
                        state.remainingMs(SystemClock.elapsedRealtime())
                            ?: return@launch
                    if (remainingMs <= 0L) break
                    delay(minOf(remainingMs, SLEEP_TIMER_CHECK_INTERVAL_MS))
                }
                if (state.deadlineElapsedRealtimeMs != expectedDeadline) return@launch
                timerJob = null
                state = state.cancel()
                broadcastState()
                player.pause()
            }
    }

    fun cancelForAccountChange() {
        if (state.deadlineElapsedRealtimeMs == null) return
        setTimer(null)
    }

    fun cancelPendingJob() {
        timerJob?.cancel()
    }

    fun currentResult(): SessionResult = SessionResult(
        SessionResult.RESULT_SUCCESS,
        PlaybackSessionCommands.sleepTimerStateExtras(state.deadlineElapsedRealtimeMs),
    )

    private fun broadcastState() {
        mediaSession().broadcastCustomCommand(
            PlaybackSessionCommands.SLEEP_TIMER_CHANGED,
            PlaybackSessionCommands.sleepTimerStateExtras(state.deadlineElapsedRealtimeMs),
        )
    }

    private companion object {
        const val SLEEP_TIMER_CHECK_INTERVAL_MS = 1_000L
    }
}
