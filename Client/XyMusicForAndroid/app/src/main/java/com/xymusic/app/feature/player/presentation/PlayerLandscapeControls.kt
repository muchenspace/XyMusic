package com.xymusic.app.feature.player.presentation

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.height
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
import androidx.compose.runtime.Composable
import androidx.compose.runtime.State
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import com.xymusic.app.feature.player.domain.model.PlaybackState
import com.xymusic.app.feature.player.domain.model.PlayerState

@Composable
internal fun LandscapeTransportControls(
    player: PlayerState,
    onTogglePlayback: () -> Unit,
    onPrevious: () -> Unit,
    onNext: () -> Unit,
    modifier: Modifier = Modifier,
    playbackPosition: State<Float>? = null,
    layoutSpec: PlayerLayoutSpec,
) {
    val interactionPosition =
        if (playbackPosition != null) {
            rememberPlaybackInteractionPositionState(playbackPosition)
        } else {
            null
        }
    val availability = rememberPlaybackControlAvailability(player, interactionPosition)
    val controlHeight = layoutSpec.landscapeTransportHeight
    val playButtonSize = layoutSpec.landscapePlayButtonSize
    val secondaryIconSize = layoutSpec.landscapeSecondaryIconSize
    val playIconSize = layoutSpec.landscapePlayIconSize
    val bufferingSize = layoutSpec.landscapeBufferingSize
    Row(
        modifier =
        modifier
            .height(controlHeight)
            .testTag(PlayerTestTags.LandscapeTransport),
        horizontalArrangement = Arrangement.SpaceEvenly,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        IconButton(
            onClick = onPrevious,
            enabled = availability.canPrevious,
            modifier = Modifier.size(48.dp).testTag(PlayerTestTags.Previous),
            colors =
            IconButtonDefaults.iconButtonColors(
                contentColor = PlayerPrimaryContent,
                disabledContentColor = PlayerMutedContent,
            ),
        ) {
            Icon(
                imageVector = Icons.Default.SkipPrevious,
                contentDescription = stringResource(R.string.player_previous),
                modifier = Modifier.size(secondaryIconSize),
            )
        }
        IconButton(
            onClick = onTogglePlayback,
            modifier = Modifier.size(playButtonSize).testTag(PlayerTestTags.TogglePlayback),
            colors = IconButtonDefaults.iconButtonColors(contentColor = PlayerPrimaryContent),
        ) {
            if (player.playbackState == PlaybackState.BUFFERING) {
                CircularProgressIndicator(
                    modifier = Modifier.size(bufferingSize),
                    color = PlayerPrimaryContent,
                    strokeWidth = 3.dp,
                )
            } else {
                Icon(
                    imageVector = if (player.isPlaying) Icons.Default.Pause else Icons.Default.PlayArrow,
                    contentDescription = stringResource(
                        if (player.isPlaying) R.string.player_pause else R.string.player_play,
                    ),
                    modifier = Modifier.size(playIconSize),
                    tint = PlayerPrimaryContent,
                )
            }
        }
        IconButton(
            onClick = onNext,
            enabled = availability.canNext,
            modifier = Modifier.size(48.dp).testTag(PlayerTestTags.Next),
            colors =
            IconButtonDefaults.iconButtonColors(
                contentColor = PlayerPrimaryContent,
                disabledContentColor = PlayerMutedContent,
            ),
        ) {
            Icon(
                imageVector = Icons.Default.SkipNext,
                contentDescription = stringResource(R.string.player_next),
                modifier = Modifier.size(secondaryIconSize),
            )
        }
    }
}
