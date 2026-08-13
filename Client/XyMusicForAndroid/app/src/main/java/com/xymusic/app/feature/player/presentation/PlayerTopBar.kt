@file:OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)

package com.xymusic.app.feature.player.presentation

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.PlaylistAdd
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.GraphicEq
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.outlined.FavoriteBorder
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import com.xymusic.app.core.ui.component.MediaArtwork
import com.xymusic.app.feature.player.domain.model.PlayerQueueItem

@Composable
internal fun PlayerTopBar(
    item: PlayerQueueItem?,
    showTrackInfo: Boolean = true,
    isFavorite: Boolean,
    onDismiss: () -> Unit,
    onToggleFavorite: () -> Unit,
    onAddToPlaylist: () -> Unit,
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
            .height(88.dp)
            .padding(horizontal = 8.dp)
            .testTag(PlayerTestTags.TopBar),
        contentAlignment = Alignment.Center,
    ) {
        IconButton(
            onClick = onDismiss,
            modifier = Modifier.size(44.dp).align(Alignment.CenterStart),
        ) {
            Icon(
                imageVector = Icons.Default.KeyboardArrowDown,
                contentDescription = stringResource(R.string.common_back),
                tint = PlayerPrimaryContent,
                modifier = Modifier.size(30.dp),
            )
        }
        if (item == null && showTrackInfo) {
            Text(
                text = stringResource(R.string.player_now_playing),
                color = PlayerPrimaryContent,
                fontWeight = FontWeight.SemiBold,
            )
        } else if (item != null && showTrackInfo) {
            Row(
                modifier =
                Modifier
                    .fillMaxWidth()
                    .padding(start = 58.dp, end = 116.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                MediaArtwork(
                    url = item.artworkUrl,
                    cacheKey = item.artworkCacheKey,
                    contentDescription = null,
                    fallbackImageRes = R.drawable.xymusic_compact,
                    modifier = Modifier.size(56.dp),
                    shape = RoundedCornerShape(14.dp),
                )
                Spacer(modifier = Modifier.width(10.dp))
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = item.title,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        color = PlayerPrimaryContent,
                        fontWeight = FontWeight.Bold,
                    )
                    Text(
                        text = item.artistNames.joinToString(" / ").ifBlank {
                            stringResource(R.string.catalog_unknown_artist)
                        },
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        color = PlayerSecondaryContent,
                    )
                }
            }
        }
        Row(
            modifier = Modifier.align(Alignment.CenterEnd).padding(end = 8.dp),
            horizontalArrangement = Arrangement.spacedBy(10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(
                onClick = onToggleFavorite,
                modifier =
                Modifier
                    .size(42.dp)
                    .clip(CircleShape)
                    .background(PlayerPrimaryContent.copy(alpha = 0.15f))
                    .testTag(PlayerTestTags.Favorite),
            ) {
                Icon(
                    imageVector = if (isFavorite) Icons.Default.Favorite else Icons.Outlined.FavoriteBorder,
                    contentDescription = stringResource(
                        if (isFavorite) R.string.library_remove_favorite else R.string.library_add_favorite,
                    ),
                    tint = PlayerPrimaryContent,
                    modifier = Modifier.size(22.dp),
                )
            }
            Box {
                IconButton(
                    onClick = { menuExpanded = true },
                    modifier =
                    Modifier
                        .size(42.dp)
                        .clip(CircleShape)
                        .background(PlayerPrimaryContent.copy(alpha = 0.15f)),
                ) {
                    Icon(
                        imageVector = Icons.Default.MoreVert,
                        contentDescription = stringResource(R.string.player_playback_options),
                        tint = PlayerPrimaryContent,
                        modifier = Modifier.size(24.dp),
                    )
                }
                DropdownMenu(
                    expanded = menuExpanded,
                    onDismissRequest = { menuExpanded = false },
                    modifier =
                    Modifier
                        .clip(RoundedCornerShape(18.dp)),
                    shape = RoundedCornerShape(18.dp),
                    containerColor = MaterialTheme.colorScheme.surfaceContainerHigh,
                    tonalElevation = 0.dp,
                    shadowElevation = 18.dp,
                ) {
                    DropdownMenuItem(
                        text = {
                            Text(
                                text = stringResource(
                                    if (isFavorite) {
                                        R.string.library_remove_favorite
                                    } else {
                                        R.string.library_add_favorite
                                    },
                                ),
                                color = PlayerPrimaryContent,
                            )
                        },
                        leadingIcon = {
                            Icon(
                                imageVector =
                                if (isFavorite) Icons.Default.Favorite else Icons.Outlined.FavoriteBorder,
                                contentDescription = null,
                                tint = PlayerPrimaryContent,
                            )
                        },
                        onClick = {
                            menuExpanded = false
                            onToggleFavorite()
                        },
                    )
                    DropdownMenuItem(
                        text = {
                            Text(
                                text = stringResource(R.string.playlist_add_track),
                                color = PlayerPrimaryContent,
                            )
                        },
                        leadingIcon = {
                            Icon(
                                imageVector = Icons.AutoMirrored.Outlined.PlaylistAdd,
                                contentDescription = null,
                                tint = PlayerPrimaryContent,
                            )
                        },
                        onClick = {
                            menuExpanded = false
                            onAddToPlaylist()
                        },
                    )
                    DropdownMenuItem(
                        text = {
                            Text(
                                text =
                                stringResource(R.string.player_playback_speed) +
                                    " - " + formatPlaybackSpeed(playbackSpeed),
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
                                text = sleepTimerRemainingMs?.let { remaining ->
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
}
