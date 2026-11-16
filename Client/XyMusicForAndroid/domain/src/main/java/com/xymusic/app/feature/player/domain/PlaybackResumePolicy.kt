package com.xymusic.app.feature.player.domain

const val RESUME_COMPLETION_TOLERANCE_MS = 1_000L

/**
 * Resolves the safe starting playback position when resuming a stored track.
 * Tracks that are already completed or within the completion tolerance at the
 * end restart from 0 ms so playback does not immediately hit EOF and skip.
 */
fun resolveResumePositionMs(positionMs: Long, durationMs: Long, completed: Boolean = false): Long {
    if (completed) return 0L
    val nonNegativePosition = positionMs.coerceAtLeast(0L)
    if (durationMs <= 0L) return nonNegativePosition
    val completedBoundaryMs = (durationMs - RESUME_COMPLETION_TOLERANCE_MS).coerceAtLeast(0L)
    return if (nonNegativePosition >= completedBoundaryMs) 0L else nonNegativePosition
}
