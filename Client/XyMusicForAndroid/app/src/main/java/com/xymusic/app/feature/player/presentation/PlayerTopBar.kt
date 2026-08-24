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
import androidx.compose.ui.graphics.vector.ImageVector
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
    Box(
        modifier =
        Modifier
            .fillMaxWidth()
            .height(88.dp)
            .padding(horizontal = 8.dp)
            .testTag(PlayerTestTags.TopBar),
        contentAlignment = Alignment.Center,
    ) {
        PlayerDismissButton(
            onClick = onDismiss,
            modifier = Modifier.align(Alignment.CenterStart),
        )
        PlayerTopBarTrackInfo(item = item, visible = showTrackInfo)
        PlayerTopBarActions(
            isFavorite = isFavorite,
            onToggleFavorite = onToggleFavorite,
            onAddToPlaylist = onAddToPlaylist,
            playbackSpeed = playbackSpeed,
            sleepTimerRemainingMs = sleepTimerRemainingMs,
            onShowSpeed = onShowSpeed,
            onShowSleepTimer = onShowSleepTimer,
            modifier = Modifier.align(Alignment.CenterEnd),
        )
    }
}

@Composable
private fun PlayerDismissButton(onClick: () -> Unit, modifier: Modifier = Modifier) {
    IconButton(
        onClick = onClick,
        modifier = modifier.size(44.dp),
    ) {
        Icon(
            imageVector = Icons.Default.KeyboardArrowDown,
            contentDescription = stringResource(R.string.common_back),
            tint = PlayerPrimaryContent,
            modifier = Modifier.size(30.dp),
        )
    }
}

@Composable
private fun PlayerTopBarTrackInfo(item: PlayerQueueItem?, visible: Boolean) {
    if (!visible) return
    if (item == null) {
        Text(
            text = stringResource(R.string.player_now_playing),
            color = PlayerPrimaryContent,
            fontWeight = FontWeight.SemiBold,
        )
        return
    }

    val artistNames = remember(item.queueItemId, item.artistNames) { item.artistNames.joinToString(" / ") }
    val artistLine = artistNames.ifBlank { stringResource(R.string.catalog_unknown_artist) }
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
            shape = PlayerTopBarArtworkShape,
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
                text = artistLine,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                color = PlayerSecondaryContent,
            )
        }
    }
}

@Composable
private fun PlayerTopBarActions(
    isFavorite: Boolean,
    onToggleFavorite: () -> Unit,
    onAddToPlaylist: () -> Unit,
    playbackSpeed: Float,
    sleepTimerRemainingMs: Long?,
    onShowSpeed: () -> Unit,
    onShowSleepTimer: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var menuExpanded by remember { mutableStateOf(false) }
    Row(
        modifier = modifier.padding(end = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        PlayerFavoriteButton(isFavorite = isFavorite, onClick = onToggleFavorite)
        Box {
            PlayerOverflowButton(onClick = { menuExpanded = true })
            PlayerOptionsMenu(
                expanded = menuExpanded,
                isFavorite = isFavorite,
                playbackSpeed = playbackSpeed,
                sleepTimerRemainingMs = sleepTimerRemainingMs,
                onDismiss = { menuExpanded = false },
                onToggleFavorite = {
                    menuExpanded = false
                    onToggleFavorite()
                },
                onAddToPlaylist = {
                    menuExpanded = false
                    onAddToPlaylist()
                },
                onShowSpeed = {
                    menuExpanded = false
                    onShowSpeed()
                },
                onShowSleepTimer = {
                    menuExpanded = false
                    onShowSleepTimer()
                },
            )
        }
    }
}

@Composable
private fun PlayerFavoriteButton(isFavorite: Boolean, onClick: () -> Unit) {
    IconButton(
        onClick = onClick,
        modifier =
        Modifier
            .size(42.dp)
            .clip(CircleShape)
            .background(PlayerPrimaryContent.copy(alpha = 0.15f))
            .testTag(PlayerTestTags.Favorite),
    ) {
        Icon(
            imageVector = if (isFavorite) Icons.Default.Favorite else Icons.Outlined.FavoriteBorder,
            contentDescription =
            stringResource(
                if (isFavorite) R.string.library_remove_favorite else R.string.library_add_favorite,
            ),
            tint = PlayerPrimaryContent,
            modifier = Modifier.size(22.dp),
        )
    }
}

@Composable
private fun PlayerOverflowButton(onClick: () -> Unit) {
    IconButton(
        onClick = onClick,
        modifier =
        Modifier
            .size(42.dp)
            .clip(CircleShape)
            .background(PlayerPrimaryContent.copy(alpha = 0.15f)),
    ) {
        Icon(
            imageVector = Icons.Default.MoreVert,
            contentDescription = stringResource(R.string.common_more_actions),
            tint = PlayerPrimaryContent,
        )
    }
}

@Composable
private fun PlayerOptionsMenu(
    expanded: Boolean,
    isFavorite: Boolean,
    playbackSpeed: Float,
    sleepTimerRemainingMs: Long?,
    onDismiss: () -> Unit,
    onToggleFavorite: () -> Unit,
    onAddToPlaylist: () -> Unit,
    onShowSpeed: () -> Unit,
    onShowSleepTimer: () -> Unit,
) {
    val favoriteLabel =
        stringResource(
            if (isFavorite) R.string.library_remove_favorite else R.string.library_add_favorite,
        )
    val playbackSpeedLabel =
        stringResource(R.string.player_playback_speed) + " - " + formatPlaybackSpeed(playbackSpeed)
    val sleepTimerLabel =
        sleepTimerRemainingMs?.let { remaining ->
            stringResource(
                R.string.player_sleep_timer_remaining,
                ((remaining + 59_999L) / 60_000L).coerceAtLeast(1L),
            )
        } ?: stringResource(R.string.player_sleep_timer)

    DropdownMenu(
        expanded = expanded,
        onDismissRequest = onDismiss,
        modifier = Modifier.clip(PlayerOptionsMenuShape),
        shape = PlayerOptionsMenuShape,
        containerColor = MaterialTheme.colorScheme.surfaceContainerHigh,
        tonalElevation = 0.dp,
        shadowElevation = 18.dp,
    ) {
        PlayerDropdownMenuItem(
            text = favoriteLabel,
            icon = if (isFavorite) Icons.Default.Favorite else Icons.Outlined.FavoriteBorder,
            onClick = onToggleFavorite,
        )
        PlayerDropdownMenuItem(
            text = stringResource(R.string.playlist_add_track),
            icon = Icons.AutoMirrored.Outlined.PlaylistAdd,
            onClick = onAddToPlaylist,
        )
        PlayerDropdownMenuItem(
            text = playbackSpeedLabel,
            icon = Icons.Default.GraphicEq,
            onClick = onShowSpeed,
        )
        PlayerDropdownMenuItem(
            text = sleepTimerLabel,
            icon = Icons.Default.Pause,
            onClick = onShowSleepTimer,
        )
    }
}

@Composable
private fun PlayerDropdownMenuItem(text: String, icon: ImageVector, onClick: () -> Unit) {
    DropdownMenuItem(
        text = { Text(text = text, color = PlayerPrimaryContent) },
        leadingIcon = {
            Icon(
                imageVector = icon,
                contentDescription = null,
                tint = PlayerPrimaryContent,
            )
        },
        onClick = onClick,
    )
}

private val PlayerTopBarArtworkShape = RoundedCornerShape(14.dp)
private val PlayerOptionsMenuShape = RoundedCornerShape(18.dp)
