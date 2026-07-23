package com.xymusic.app.feature.player.presentation

import android.os.SystemClock
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.State
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import com.xymusic.app.feature.player.domain.model.PlayerState
import kotlinx.coroutines.isActive
import kotlin.math.abs

@Composable
internal fun rememberPlaybackPositionState(player: PlayerState): State<Float> {
    val displayedPosition = remember { mutableFloatStateOf(player.positionMs.toFloat()) }
    var previousSample by remember { mutableStateOf<PlaybackPositionClockSample?>(null) }

    LaunchedEffect(
        player.currentQueueItemId,
        player.positionMs,
        player.positionAnchorElapsedRealtimeMs,
        player.positionDiscontinuitySequence,
        player.isPlaying,
        player.playbackSpeed,
        player.durationMs,
    ) {
        val nowElapsedRealtimeMs = SystemClock.elapsedRealtime()
        if (player.positionAnchorElapsedRealtimeMs == null) {
            displayedPosition.floatValue = player.positionMs.toFloat()
            previousSample = PlaybackPositionClockSample.from(player)
            return@LaunchedEffect
        }
        val clockPlayer = player
        val targetPosition = anchoredPlaybackPositionMs(clockPlayer, nowElapsedRealtimeMs)
        val shouldSnap =
            shouldSnapPlaybackPosition(
                previousSample = previousSample,
                player = clockPlayer,
                displayedPositionMs = displayedPosition.floatValue,
                nowElapsedRealtimeMs = nowElapsedRealtimeMs,
            )
        val correction =
            if (shouldSnap) {
                null
            } else {
                playbackPositionCorrection(
                    displayedPositionMs = displayedPosition.floatValue,
                    targetPositionMs = targetPosition,
                    startElapsedRealtimeMs = nowElapsedRealtimeMs,
                )
            }
        if (shouldSnap) displayedPosition.floatValue = targetPosition
        previousSample = PlaybackPositionClockSample.from(player)

        if (!player.isPlaying) return@LaunchedEffect
        while (isActive) {
            withFrameNanos {
                val frameElapsedRealtimeMs = SystemClock.elapsedRealtime()
                val basePosition = anchoredPlaybackPositionMs(clockPlayer, frameElapsedRealtimeMs)
                val correctedPosition =
                    basePosition + (correction?.remainingOffset(frameElapsedRealtimeMs) ?: 0f)
                displayedPosition.floatValue =
                    monotonicPlaybackPosition(
                        previousPositionMs = displayedPosition.floatValue,
                        candidatePositionMs =
                        correctedPosition.clampPlaybackPosition(durationMs = player.durationMs),
                    )
            }
        }
    }
    return displayedPosition
}

internal fun normalizedPlaybackProgress(positionMs: Float, durationMs: Long): Float = if (durationMs > 0L) {
    (positionMs / durationMs).coerceIn(0f, 1f)
} else {
    0f
}

internal fun anchoredPlaybackPositionMs(player: PlayerState, nowElapsedRealtimeMs: Long): Float {
    val anchorElapsedRealtimeMs = player.positionAnchorElapsedRealtimeMs ?: nowElapsedRealtimeMs
    val elapsedMs = (nowElapsedRealtimeMs - anchorElapsedRealtimeMs).coerceAtLeast(0L)
    val advancedPosition =
        if (player.isPlaying) {
            player.positionMs + elapsedMs * player.playbackSpeed.coerceAtLeast(0f)
        } else {
            player.positionMs.toFloat()
        }
    return advancedPosition.clampPlaybackPosition(durationMs = player.durationMs)
}

internal data class PlaybackPositionClockSample(
    val currentQueueItemId: String?,
    val discontinuitySequence: Long,
    val isPlaying: Boolean,
) {
    companion object {
        fun from(player: PlayerState) =
            PlaybackPositionClockSample(
                currentQueueItemId = player.currentQueueItemId,
                discontinuitySequence = player.positionDiscontinuitySequence,
                isPlaying = player.isPlaying,
            )
    }
}

internal fun shouldSnapPlaybackPosition(
    previousSample: PlaybackPositionClockSample?,
    player: PlayerState,
    displayedPositionMs: Float,
    nowElapsedRealtimeMs: Long,
): Boolean {
    if (previousSample == null || !player.isPlaying) return true
    if (previousSample.currentQueueItemId != player.currentQueueItemId) return true
    if (previousSample.discontinuitySequence != player.positionDiscontinuitySequence) return true
    if (previousSample.isPlaying != player.isPlaying) return true
    val targetPosition = anchoredPlaybackPositionMs(player, nowElapsedRealtimeMs)
    return abs(targetPosition - displayedPositionMs) > PLAYBACK_POSITION_SNAP_THRESHOLD_MS
}

internal fun monotonicPlaybackPosition(previousPositionMs: Float, candidatePositionMs: Float): Float =
    maxOf(previousPositionMs, candidatePositionMs)

internal fun playbackLyricIndex(lines: List<PlayerLyricLineUi>, positionMs: Long): Int {
    var low = 0
    var high = lines.lastIndex
    var result = -1
    while (low <= high) {
        val middle = (low + high).ushr(1)
        val timeMs = lines[middle].timeMs ?: return -1
        if (timeMs <= positionMs) {
            result = middle
            low = middle + 1
        } else {
            high = middle - 1
        }
    }
    return result
}

private data class PlaybackPositionCorrection(
    val offsetMs: Float,
    val startElapsedRealtimeMs: Long,
) {
    fun remainingOffset(nowElapsedRealtimeMs: Long): Float {
        val progress =
            ((nowElapsedRealtimeMs - startElapsedRealtimeMs).toFloat() / PLAYBACK_POSITION_CORRECTION_MS)
                .coerceIn(0f, 1f)
        return offsetMs * (1f - FastOutSlowInEasing.transform(progress))
    }
}

private fun playbackPositionCorrection(
    displayedPositionMs: Float,
    targetPositionMs: Float,
    startElapsedRealtimeMs: Long,
): PlaybackPositionCorrection? {
    val offsetMs = displayedPositionMs - targetPositionMs
    return if (abs(offsetMs) > PLAYBACK_POSITION_CORRECTION_EPSILON_MS) {
        PlaybackPositionCorrection(
            offsetMs = offsetMs,
            startElapsedRealtimeMs = startElapsedRealtimeMs,
        )
    } else {
        null
    }
}

private fun Float.clampPlaybackPosition(durationMs: Long): Float =
    coerceAtMost(durationMs.takeIf { it > 0 }?.toFloat() ?: Float.MAX_VALUE).coerceAtLeast(0f)

private const val PLAYBACK_POSITION_CORRECTION_MS = 120f
private const val PLAYBACK_POSITION_CORRECTION_EPSILON_MS = 0.5f
private const val PLAYBACK_POSITION_SNAP_THRESHOLD_MS = 250f
