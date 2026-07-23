@file:OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)

package com.xymusic.app.feature.player.presentation

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.GraphicEq
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.xymusic.app.R

@Composable
internal fun PlayerTopBar(
    onDismiss: () -> Unit,
    playbackSpeed: Float,
    sleepTimerRemainingMs: Long?,
    onShowSpeed: () -> Unit,
    onShowSleepTimer: () -> Unit,
) {
    var menuExpanded by remember { mutableStateOf(false) }
    Box(
        modifier =
        Modifier
            .fillMaxWidth()
            .height(60.dp)
            .padding(horizontal = 14.dp)
            .testTag(PlayerTestTags.TopBar),
    ) {
        IconButton(
            onClick = onDismiss,
            modifier = Modifier.size(44.dp).align(Alignment.CenterStart),
        ) {
            Icon(
                Icons.Default.KeyboardArrowDown,
                contentDescription = stringResource(R.string.common_back),
                tint = PlayerPrimaryContent,
                modifier = Modifier.size(34.dp),
            )
        }
        Text(
            text = stringResource(R.string.player_now_playing),
            modifier = Modifier.align(Alignment.Center),
            color = PlayerPrimaryContent,
            style = MaterialTheme.typography.labelLarge,
            fontWeight = FontWeight.SemiBold,
        )
        Box(modifier = Modifier.align(Alignment.CenterEnd)) {
            IconButton(
                onClick = { menuExpanded = true },
                modifier = Modifier.size(44.dp),
            ) {
                Icon(
                    Icons.Default.MoreVert,
                    contentDescription = stringResource(R.string.player_playback_options),
                    tint = PlayerPrimaryContent,
                    modifier = Modifier.size(26.dp),
                )
            }
            DropdownMenu(
                expanded = menuExpanded,
                onDismissRequest = { menuExpanded = false },
                modifier = Modifier.background(MaterialTheme.colorScheme.surfaceContainerHigh),
            ) {
                DropdownMenuItem(
                    text = {
                        Text(
                            text =
                            stringResource(R.string.player_playback_speed) +
                                " · " + formatPlaybackSpeed(playbackSpeed),
                            color = PlayerPrimaryContent,
                        )
                    },
                    leadingIcon = {
                        Icon(Icons.Default.GraphicEq, contentDescription = null, tint = PlayerPrimaryContent)
                    },
                    onClick = {
                        menuExpanded = false
                        onShowSpeed()
                    },
                )
                DropdownMenuItem(
                    text = {
                        Text(
                            text =
                            sleepTimerRemainingMs?.let { remaining ->
                                stringResource(
                                    R.string.player_sleep_timer_remaining,
                                    ((remaining + 59_999L) / 60_000L).coerceAtLeast(1L),
                                )
                            } ?: stringResource(R.string.player_sleep_timer),
                            color = PlayerPrimaryContent,
                        )
                    },
                    leadingIcon = {
                        Icon(Icons.Default.Pause, contentDescription = null, tint = PlayerPrimaryContent)
                    },
                    onClick = {
                        menuExpanded = false
                        onShowSleepTimer()
                    },
                )
            }
        }
    }
}
