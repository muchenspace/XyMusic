@file:OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)

package com.xymusic.app.feature.player.presentation

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import com.xymusic.app.R

@Composable
internal fun PlayerAlertDialog(
    onDismissRequest: () -> Unit,
    title: String,
    message: String,
    confirmLabel: String,
    onConfirm: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismissRequest,
        containerColor = MaterialTheme.colorScheme.surfaceContainerHigh,
        title = { Text(title, color = PlayerPrimaryContent) },
        text = { Text(message, color = PlayerSecondaryContent) },
        confirmButton = {
            TextButton(onClick = onConfirm) { Text(confirmLabel, color = PlayerPrimaryContent) }
        },
        dismissButton = {
            TextButton(onClick = onDismissRequest) {
                Text(stringResource(R.string.common_cancel), color = PlayerSecondaryContent)
            }
        },
    )
}

@Composable
internal fun PlayerChoiceDialog(
    title: String,
    options: List<String>,
    selectedIndex: Int,
    onSelect: (Int) -> Unit,
    onDismiss: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = MaterialTheme.colorScheme.surfaceContainerHigh,
        title = { Text(title, color = PlayerPrimaryContent) },
        text = {
            Column {
                options.forEachIndexed { index, option ->
                    TextButton(
                        onClick = { onSelect(index) },
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        Text(
                            text = option,
                            color = PlayerPrimaryContent,
                            fontWeight = if (index == selectedIndex) FontWeight.Bold else FontWeight.Normal,
                        )
                    }
                }
            }
        },
        confirmButton = {},
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(stringResource(R.string.common_cancel), color = PlayerSecondaryContent)
            }
        },
    )
}

internal fun formatPlaybackSpeed(speed: Float): String = if (speed % 1f == 0f) "${speed.toInt()}.0x" else "${speed}x"

internal val PLAYBACK_SPEED_OPTIONS = listOf(0.5f, 0.75f, 1f, 1.25f, 1.5f, 2f)
internal val SLEEP_TIMER_MINUTE_OPTIONS = listOf(15, 30, 60)

internal object PlayerTestTags {
    const val MiniBar = "player_mini_bar"
    const val TogglePlayback = "player_toggle_playback"
    const val OpenPlayer = "player_open_fullscreen"
    const val ArtworkPlaceholder = "player_artwork_placeholder"
    const val ArtworkImage = "player_artwork_image"
    const val Next = "player_next"
    const val Previous = "player_previous"
    const val Favorite = "player_favorite"
    const val ContentPager = "player_content_pager"
    const val TopBar = "player_top_bar"
    const val LandscapeArtworkPane = "player_landscape_artwork_pane"
    const val LandscapeArtwork = "player_landscape_artwork"
    const val LandscapeLyricsPane = "player_landscape_lyrics_pane"
    const val LandscapeTrackHeader = "player_landscape_track_header"
    const val LandscapePlaybackBar = "player_landscape_playback_bar"
    const val LandscapeTimeline = "player_landscape_timeline"
    const val LandscapeTime = "player_landscape_time"
    const val LandscapeTransport = "player_landscape_transport"
    const val QueueContent = "player_queue_content"
}
