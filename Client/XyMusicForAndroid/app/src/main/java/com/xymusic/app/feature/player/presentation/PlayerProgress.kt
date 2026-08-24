package com.xymusic.app.feature.player.presentation

import android.os.SystemClock
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.State
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.withFrameNanos
import com.xymusic.app.feature.player.domain.model.PlayerState
import java.util.concurrent.atomic.AtomicReference
import kotlin.math.abs
import kotlinx.coroutines.Job
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

@Composable
internal fun rememberPlaybackPositionSnapshotState(player: PlayerState): State<Float> {
    val displayedPosition = remember { mutableFloatStateOf(player.positionMs.toFloat()) }
    LaunchedEffect(player.currentQueueItemId, player.positionDiscontinuitySequence, player.positionMs) {
        displayedPosition.floatValue = player.positionMs.toFloat()
    }
    return displayedPosition
}

/**
 * Projects the frame-rate position into the coarse value used by composition
 * state such as slider semantics and elapsed-time labels.
 */
@Composable
internal fun rememberPlaybackInteractionPositionState(playbackPosition: State<Float>): State<Float> =
    remember(playbackPosition) {
        derivedStateOf { playbackInteractionPositionMs(playbackPosition.value) }
    }

/**
 * Creates the single playback clock used by the navigation tree.
 *
 * The flow collector only updates the latest player sample. A frame ticker is
 * launched while playback is active, so paused screens do not keep a frame
 * coroutine alive and multiple consumers do not create independent tickers.
 */
@Composable
internal fun rememberPlaybackPositionState(playerFlow: StateFlow<PlayerState>): State<Float> {
    val clock = remember(playerFlow) { PlaybackPositionClock(playerFlow.value) }
    LaunchedEffect(playerFlow, clock) {
        var ticker: Job? = null
        try {
            playerFlow.collect { player ->
                clock.update(player)
                if (player.isPlaying) {
                    if (ticker?.isActive != true) {
                        ticker = launch { clock.runWhilePlaying() }
                    }
                } else {
                    ticker?.cancel()
                    ticker = null
                    clock.syncToLatestPlayer()
                }
            }
        } finally {
            ticker?.cancel()
        }
    }
    return clock.position
}

private class PlaybackPositionClock(initialPlayer: PlayerState) {
    val position = mutableFloatStateOf(
        initialPlaybackPositionMs(initialPlayer, SystemClock.elapsedRealtimeNanos()),
    )
    private val latestPlayer = AtomicReference(initialPlayer)
    private var hasPlayerSample = true

    fun update(player: PlayerState) {
        val previous = latestPlayer.getAndSet(player)
        val discontinuity =
            !hasPlayerSample ||
                previous.currentQueueItemId != player.currentQueueItemId ||
                previous.positionDiscontinuitySequence != player.positionDiscontinuitySequence
        hasPlayerSample = true
        // Publish a queue/seek boundary immediately. Waiting for the next frame leaves the new
        // lyric document paired with the previous track's position for one visible frame.
        if (discontinuity || !player.isPlaying) {
            position.floatValue = player.positionMs.toFloat()
        }
    }

    fun syncToLatestPlayer() {
        position.floatValue = latestPlayer.get().positionMs.toFloat()
    }

    suspend fun runWhilePlaying() {
        val coroutineContext = currentCoroutineContext()
        val loopState = PlaybackPositionLoopState()
        while (coroutineContext.isActive) {
            val currentPlayer = latestPlayer.get()
            if (!currentPlayer.isPlaying) return
            loopState
                .synchronizePlayerSample(
                    player = currentPlayer,
                    displayedPositionMs = position.floatValue,
                )?.let { snappedPosition -> position.floatValue = snappedPosition }
            withFrameNanos {
                position.floatValue =
                    loopState.renderFrame(
                        sampledPlayer = currentPlayer,
                        latestPlayer = latestPlayer.get(),
                        displayedPositionMs = position.floatValue,
                    )
            }
        }
    }
}

private class PlaybackPositionLoopState {
    private var previousSample: PlaybackPositionClockSample? = null
    private var lastAnchorElapsedRealtimeMs: Long? = null
    private var lastPositionMs = Long.MIN_VALUE
    private var correction: PlaybackPositionCorrection? = null

    fun synchronizePlayerSample(player: PlayerState, displayedPositionMs: Float): Float? {
        val currentSample = PlaybackPositionClockSample.from(player)
        if (!hasNewPlayerSample(player, currentSample)) return null

        val nowElapsedRealtimeMs = SystemClock.elapsedRealtime()
        val targetPosition = anchoredPlaybackPositionMs(player, nowElapsedRealtimeMs)
        val shouldSnap =
            shouldSnapPlaybackPosition(
                previousSample = previousSample,
                player = player,
                displayedPositionMs = displayedPositionMs,
                nowElapsedRealtimeMs = nowElapsedRealtimeMs,
            )
        correction =
            if (shouldSnap) {
                null
            } else {
                playbackPositionCorrection(
                    displayedPositionMs = displayedPositionMs,
                    targetPositionMs = targetPosition,
                    startElapsedRealtimeMs = nowElapsedRealtimeMs,
                    generation = PlaybackPositionGeneration.from(player),
                )
            }
        previousSample = currentSample
        lastAnchorElapsedRealtimeMs = player.positionAnchorElapsedRealtimeMs
        lastPositionMs = player.positionMs
        return targetPosition.takeIf { shouldSnap }
    }

    fun renderFrame(sampledPlayer: PlayerState, latestPlayer: PlayerState, displayedPositionMs: Float): Float {
        if (hasPlaybackSampleDiscontinuity(sampledPlayer, latestPlayer)) {
            correction = null
            return anchoredPlaybackPositionMsPrecise(latestPlayer, SystemClock.elapsedRealtimeNanos())
        }

        // elapsedRealtime() is millisecond based. At 120/144 Hz that quantises a frame's
        // movement into visible 1 ms steps, especially at slower playback speeds.
        val frameElapsedRealtimeNanos = SystemClock.elapsedRealtimeNanos()
        // Keep this frame on the sample captured before suspension. A routine anchor update that
        // arrives while awaiting VSync is picked up at the top of the next loop, where its
        // correction can be calculated instead of becoming a one-frame jump.
        val basePosition = anchoredPlaybackPositionMsPrecise(sampledPlayer, frameElapsedRealtimeNanos)
        val frameGeneration = PlaybackPositionGeneration.from(sampledPlayer)
        val frameCorrection = correction?.takeIf { it.generation == frameGeneration }
        if (frameCorrection == null && correction != null) {
            correction = null
            return basePosition
        }
        val correctedPosition =
            basePosition +
                (frameCorrection?.remainingOffset(frameElapsedRealtimeNanos / NANOS_PER_MILLISECOND) ?: 0f)
        return renderedPlaybackPosition(
            previousPositionMs = displayedPositionMs,
            candidatePositionMs = correctedPosition.clampPlaybackPosition(durationMs = sampledPlayer.durationMs),
        )
    }

    private fun hasNewPlayerSample(player: PlayerState, currentSample: PlaybackPositionClockSample): Boolean =
        previousSample == null ||
            previousSample != currentSample ||
            lastAnchorElapsedRealtimeMs != player.positionAnchorElapsedRealtimeMs ||
            lastPositionMs != player.positionMs
}

internal fun initialPlaybackPositionMs(player: PlayerState, nowElapsedRealtimeNanos: Long): Float =
    anchoredPlaybackPositionMsPrecise(player, nowElapsedRealtimeNanos)

internal fun anchoredPlaybackPositionMsPrecise(player: PlayerState, nowElapsedRealtimeNanos: Long): Float {
    val anchorElapsedRealtimeMs = player.positionAnchorElapsedRealtimeMs
    if (!player.isPlaying || anchorElapsedRealtimeMs == null) return player.positionMs.toFloat()
    val anchorElapsedRealtimeNanos = anchorElapsedRealtimeMs * NANOS_PER_MILLISECOND
    val elapsedMs =
        ((nowElapsedRealtimeNanos - anchorElapsedRealtimeNanos).coerceAtLeast(0L)).toDouble() /
            NANOS_PER_MILLISECOND.toDouble()
    return (player.positionMs.toDouble() + elapsedMs * player.playbackSpeed.coerceAtLeast(0f))
        .toFloat()
        .clampPlaybackPosition(player.durationMs)
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
        fun from(player: PlayerState) = PlaybackPositionClockSample(
            currentQueueItemId = player.currentQueueItemId,
            discontinuitySequence = player.positionDiscontinuitySequence,
            isPlaying = player.isPlaying,
        )
    }
}

internal fun hasPlaybackSampleDiscontinuity(current: PlayerState, latest: PlayerState): Boolean =
    PlaybackPositionClockSample.from(current) != PlaybackPositionClockSample.from(latest)

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

/**
 * Keeps the visual clock monotonic. Explicit seeks are applied before the
 * frame update when the player reports a discontinuity.
 */
internal fun renderedPlaybackPosition(previousPositionMs: Float, candidatePositionMs: Float): Float =
    monotonicPlaybackPosition(previousPositionMs, candidatePositionMs)

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

private data class PlaybackPositionGeneration(
    val currentQueueItemId: String?,
    val discontinuitySequence: Long,
    val positionMs: Long,
    val positionAnchorElapsedRealtimeMs: Long?,
    val playbackSpeed: Float,
    val isPlaying: Boolean,
) {
    companion object {
        fun from(player: PlayerState) = PlaybackPositionGeneration(
            currentQueueItemId = player.currentQueueItemId,
            discontinuitySequence = player.positionDiscontinuitySequence,
            positionMs = player.positionMs,
            positionAnchorElapsedRealtimeMs = player.positionAnchorElapsedRealtimeMs,
            playbackSpeed = player.playbackSpeed,
            isPlaying = player.isPlaying,
        )
    }
}

private data class PlaybackPositionCorrection(
    val offsetMs: Float,
    val startElapsedRealtimeMs: Long,
    val generation: PlaybackPositionGeneration,
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
    generation: PlaybackPositionGeneration,
): PlaybackPositionCorrection? {
    val offsetMs = displayedPositionMs - targetPositionMs
    return if (abs(offsetMs) > PLAYBACK_POSITION_CORRECTION_EPSILON_MS) {
        PlaybackPositionCorrection(
            offsetMs = offsetMs,
            startElapsedRealtimeMs = startElapsedRealtimeMs,
            generation = generation,
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
private const val NANOS_PER_MILLISECOND = 1_000_000L
