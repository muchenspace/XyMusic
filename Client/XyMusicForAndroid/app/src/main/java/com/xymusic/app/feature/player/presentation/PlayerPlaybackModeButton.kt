package com.xymusic.app.feature.player.presentation

import androidx.annotation.StringRes
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Repeat
import androidx.compose.material.icons.filled.RepeatOne
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import com.xymusic.app.feature.player.domain.model.RepeatMode

@Composable
internal fun PlayerPlaybackModeButton(
    shuffleEnabled: Boolean,
    repeatMode: RepeatMode,
    onClick: () -> Unit,
    showLabel: Boolean,
    modifier: Modifier = Modifier,
) {
    val visual = playbackModeVisual(shuffleEnabled, repeatMode)
    val modeDescription = stringResource(visual.labelRes)
    val actionDescription = stringResource(R.string.player_playback_mode)
    if (showLabel) {
        Row(
            modifier =
            modifier
                .height(42.dp)
                .widthIn(min = 112.dp)
                .clip(RoundedCornerShape(21.dp))
                .background(PlayerSubtleContent)
                .clickable(onClick = onClick)
                .padding(horizontal = 16.dp)
                .semantics {
                    contentDescription = actionDescription
                    stateDescription = modeDescription
                },
            horizontalArrangement = Arrangement.Center,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(
                imageVector = visual.icon,
                contentDescription = null,
                modifier = Modifier.size(21.dp),
                tint = PlayerPrimaryContent,
            )
            Text(
                text = stringResource(visual.labelRes),
                modifier = Modifier.padding(start = 7.dp),
                color = PlayerPrimaryContent,
                style = MaterialTheme.typography.labelMedium,
            )
        }
    } else {
        IconButton(
            onClick = onClick,
            modifier =
            modifier
                .size(44.dp)
                .semantics { stateDescription = modeDescription },
        ) {
            Icon(
                imageVector = visual.icon,
                contentDescription = actionDescription,
                tint = PlayerPrimaryContent,
                modifier = Modifier.size(24.dp),
            )
        }
    }
}

private data class PlaybackModeVisual(val icon: ImageVector, @StringRes val labelRes: Int)

private fun playbackModeVisual(shuffleEnabled: Boolean, repeatMode: RepeatMode): PlaybackModeVisual = when {
    shuffleEnabled -> PlaybackModeVisual(Icons.Default.Shuffle, R.string.player_shuffle)
    repeatMode == RepeatMode.ONE -> PlaybackModeVisual(Icons.Default.RepeatOne, R.string.player_repeat_one)
    repeatMode == RepeatMode.ALL -> PlaybackModeVisual(Icons.Default.Repeat, R.string.player_repeat_all)
    else -> PlaybackModeVisual(Icons.Default.Repeat, R.string.player_repeat_off)
}
