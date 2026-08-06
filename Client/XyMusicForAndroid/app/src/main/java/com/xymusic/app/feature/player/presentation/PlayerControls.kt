package com.xymusic.app.feature.player.presentation

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material.icons.filled.SkipPrevious
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.IconButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.State
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import com.xymusic.app.core.ui.component.XySlider
import com.xymusic.app.feature.player.domain.model.PlaybackState
import com.xymusic.app.feature.player.domain.model.PlayerState
import java.util.Locale

@Composable
internal fun PlaybackControls(
    uiState: PlayerUiState,
    draggedPosition: Float?,
    onPositionChange: (Float) -> Unit,
    onPositionChangeFinished: () -> Unit,
    onTogglePlayback: () -> Unit,
    onPrevious: () -> Unit,
    onNext: () -> Unit,
    compact: Boolean = false,
    playbackPosition: State<Float>? = null,
) {
    val interactionPosition =
        if (playbackPosition != null) {
            rememberPlaybackInteractionPositionState(playbackPosition)
        } else {
            null
        }
    val availability = rememberPlaybackControlAvailability(uiState.player, interactionPosition)
    val horizontalPadding = if (compact) 22.dp else 28.dp
    val skipSize = if (compact) 54.dp else 64.dp
    val playSize = if (compact) 64.dp else 76.dp

    Column(
        modifier =
        Modifier
            .fillMaxWidth()
            .padding(horizontal = horizontalPadding)
            .padding(top = 4.dp, bottom = if (compact) 2.dp else 6.dp),
    ) {
        PlaybackTimeline(
            player = uiState.player,
            draggedPosition = draggedPosition,
            onPositionChange = onPositionChange,
            onPositionChangeFinished = onPositionChangeFinished,
            compact = compact,
            playbackPosition = playbackPosition,
            interactionPosition = interactionPosition,
        )
        Spacer(modifier = Modifier.height(if (compact) 2.dp else 6.dp))
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.Center,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(
                onClick = onPrevious,
                enabled = availability.canPrevious,
                modifier = Modifier.size(skipSize),
                colors =
                IconButtonDefaults.iconButtonColors(
                    contentColor = PlayerPrimaryContent,
                    disabledContentColor = PlayerMutedContent,
                ),
            ) {
                Icon(
                    imageVector = Icons.Default.SkipPrevious,
                    contentDescription = stringResource(R.string.player_previous),
                    modifier = Modifier.size(if (compact) 35.dp else 40.dp),
                )
            }
            Spacer(modifier = Modifier.weight(0.22f))
            IconButton(
                onClick = onTogglePlayback,
                modifier = Modifier.size(playSize).testTag(PlayerTestTags.TogglePlayback),
                colors = IconButtonDefaults.iconButtonColors(contentColor = PlayerPrimaryContent),
            ) {
                if (uiState.player.playbackState == PlaybackState.BUFFERING) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(if (compact) 32.dp else 38.dp),
                        color = PlayerPrimaryContent,
                        strokeWidth = 3.dp,
                    )
                } else {
                    Icon(
                        imageVector = if (uiState.player.isPlaying) Icons.Default.Pause else Icons.Default.PlayArrow,
                        contentDescription = stringResource(
                            if (uiState.player.isPlaying) R.string.player_pause else R.string.player_play,
                        ),
                        modifier = Modifier.size(if (compact) 45.dp else 52.dp),
                        tint = PlayerPrimaryContent,
                    )
                }
            }
            Spacer(modifier = Modifier.weight(0.22f))
            IconButton(
                onClick = onNext,
                enabled = availability.canNext,
                modifier = Modifier.size(skipSize),
                colors =
                IconButtonDefaults.iconButtonColors(
                    contentColor = PlayerPrimaryContent,
                    disabledContentColor = PlayerMutedContent,
                ),
            ) {
                Icon(
                    imageVector = Icons.Default.SkipNext,
                    contentDescription = stringResource(R.string.player_next),
                    modifier = Modifier.size(if (compact) 35.dp else 40.dp),
                )
            }
        }
    }
}

@Composable
internal fun rememberPlaybackControlAvailability(
    player: PlayerState,
    playbackPosition: State<Float>? = null,
): PlaybackControlAvailability {
    val availability =
        remember(
            player.queue,
            player.currentQueueItemId,
            player.positionMs.takeIf { playbackPosition == null },
            player.shuffleEnabled,
            player.repeatMode,
            playbackPosition,
        ) {
            derivedStateOf {
                val currentIndex = player.queue.indexOfFirst { it.queueItemId == player.currentQueueItemId }
                val hasMultipleItems = player.queue.size > 1
                val positionMs = playbackPosition?.value?.toLong() ?: player.positionMs
                PlaybackControlAvailability(
                    canPrevious =
                    positionMs > 3_000L ||
                        (hasMultipleItems && currentIndex >= 0),
                    canNext = hasMultipleItems && currentIndex >= 0,
                )
            }
        }
    return availability.value
}

internal data class PlaybackControlAvailability(val canPrevious: Boolean, val canNext: Boolean)

@Composable
private fun PlaybackTimeline(
    player: PlayerState,
    draggedPosition: Float?,
    onPositionChange: (Float) -> Unit,
    onPositionChangeFinished: () -> Unit,
    compact: Boolean,
    playbackPosition: State<Float>?,
    interactionPosition: State<Float>?,
) {
    val duration = player.durationMs.coerceAtLeast(0)
    val visualPosition = playbackPosition ?: rememberPlaybackPositionSnapshotState(player)
    val compositionPosition = interactionPosition ?: visualPosition

    PlaybackTimelineSlider(
        durationMs = duration,
        visualPosition = visualPosition,
        interactionPosition = compositionPosition,
        draggedPosition = draggedPosition,
        onPositionChange = onPositionChange,
        onPositionChangeFinished = onPositionChangeFinished,
        compact = compact,
    )
    PlaybackTimeLabels(
        durationMs = duration,
        positionState = compositionPosition,
        draggedPosition = draggedPosition,
    )
}

@Composable
private fun PlaybackTimelineSlider(
    durationMs: Long,
    visualPosition: State<Float>,
    interactionPosition: State<Float>,
    draggedPosition: Float?,
    onPositionChange: (Float) -> Unit,
    onPositionChangeFinished: () -> Unit,
    compact: Boolean,
) {
    val sliderValue = draggedPosition ?: interactionPosition.value
    XySlider(
        value = sliderValue.coerceIn(0f, durationMs.coerceAtLeast(1).toFloat()),
        onValueChange = onPositionChange,
        onValueChangeFinished = onPositionChangeFinished,
        valueRange = 0f..durationMs.coerceAtLeast(1).toFloat(),
        enabled = durationMs > 0,
        compact = compact,
        thumbSize = if (compact) 7f else 8f,
        trackHeight = 3f,
        activeColor = PlayerPrimaryContent,
        inactiveColor = PlayerSubtleContent,
        visualValue = draggedPosition?.let { null } ?: visualPosition,
    )
}

@Composable
private fun PlaybackTimeLabels(durationMs: Long, positionState: State<Float>, draggedPosition: Float?) {
    val elapsedSecond by remember(positionState, draggedPosition) {
        derivedStateOf {
            (draggedPosition ?: positionState.value).toLong().coerceAtLeast(0L) / 1_000L
        }
    }
    val remainingSecond by remember(positionState, draggedPosition, durationMs) {
        derivedStateOf {
            (durationMs - (draggedPosition ?: positionState.value).toLong()).coerceAtLeast(0L) / 1_000L
        }
    }
    val elapsedText = remember(elapsedSecond) { formatPlaybackTime(elapsedSecond * 1_000L) }
    val remainingText =
        remember(remainingSecond, durationMs) {
            if (durationMs > 0L) "-${formatPlaybackTime(remainingSecond * 1_000L)}" else "0:00"
        }

    Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 2.dp)) {
        Text(
            text = elapsedText,
            color = PlayerSecondaryContent,
            style = MaterialTheme.typography.labelSmall,
        )
        Spacer(modifier = Modifier.weight(1f))
        Text(
            text = remainingText,
            color = PlayerSecondaryContent,
            style = MaterialTheme.typography.labelSmall,
        )
    }
}

internal fun formatPlaybackTime(durationMs: Long): String {
    val totalSeconds = durationMs.coerceAtLeast(0) / 1_000
    val hours = totalSeconds / 3_600
    val minutes = (totalSeconds % 3_600) / 60
    val seconds = totalSeconds % 60
    return if (hours > 0) {
        String.format(Locale.getDefault(), "%d:%02d:%02d", hours, minutes, seconds)
    } else {
        String.format(Locale.getDefault(), "%d:%02d", minutes, seconds)
    }
}

internal fun playbackInteractionPositionMs(positionMs: Float): Float {
    if (!positionMs.isFinite() || positionMs <= 0f) return 0f
    return (positionMs.toLong() / PLAYBACK_POSITION_INTERACTION_QUANTUM_MS) *
        PLAYBACK_POSITION_INTERACTION_QUANTUM_MS.toFloat()
}

private const val PLAYBACK_POSITION_INTERACTION_QUANTUM_MS = 1_000L
