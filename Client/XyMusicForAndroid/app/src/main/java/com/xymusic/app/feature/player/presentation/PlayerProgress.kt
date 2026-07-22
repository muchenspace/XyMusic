package com.xymusic.app.feature.player.presentation

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.snap
import androidx.compose.animation.core.tween
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.State
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import com.xymusic.app.feature.player.domain.model.PlayerState
import kotlin.math.abs
import kotlin.math.roundToLong

@Composable
internal fun rememberSmoothedPlaybackPositionState(player: PlayerState): State<Float> {
    var previousAnchor by remember(player.currentQueueItemId) {
        mutableLongStateOf(player.positionMs)
    }
    val anchorDelta = player.positionMs - previousAnchor
    val discontinuity = abs(anchorDelta) > POSITION_DISCONTINUITY_THRESHOLD_MS
    val target = projectedPlaybackPositionMs(player).toFloat()
    val position =
        animateFloatAsState(
            targetValue = target,
            animationSpec =
            if (player.isPlaying && !discontinuity) {
                tween(POSITION_SAMPLE_INTERVAL_MS.toInt(), easing = LinearEasing)
            } else {
                snap()
            },
            label = "playbackPosition",
        )
    LaunchedEffect(player.positionMs) {
        previousAnchor = player.positionMs
    }
    return position
}

internal fun normalizedPlaybackProgress(positionMs: Float, durationMs: Long): Float = if (durationMs > 0L) {
    (positionMs / durationMs).coerceIn(0f, 1f)
} else {
    0f
}

internal fun projectedPlaybackPositionMs(player: PlayerState): Long {
    if (!player.isPlaying) return player.positionMs.coerceAtLeast(0)
    val projectedAdvance =
        (POSITION_SAMPLE_INTERVAL_MS * player.playbackSpeed.coerceAtLeast(0f)).roundToLong()
    return (player.positionMs + projectedAdvance)
        .coerceAtMost(player.durationMs.takeIf { it > 0 } ?: Long.MAX_VALUE)
        .coerceAtLeast(0)
}

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

private const val POSITION_SAMPLE_INTERVAL_MS = 1_000L
private const val POSITION_DISCONTINUITY_THRESHOLD_MS = 2_000L
