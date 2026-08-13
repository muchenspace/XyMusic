package com.xymusic.app.feature.player.presentation

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.PlaylistAdd
import androidx.compose.material.icons.outlined.MusicNote
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import com.xymusic.app.core.ui.component.MediaArtwork
import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
import com.xymusic.app.feature.player.domain.model.RepeatMode

@Composable
internal fun NowPlayingContent(
    item: PlayerQueueItem,
    shuffleEnabled: Boolean,
    repeatMode: RepeatMode,
    onCyclePlaybackMode: () -> Unit,
    onAddToPlaylist: () -> Unit,
    wideLayout: Boolean,
    modifier: Modifier = Modifier,
) {
    BoxWithConstraints(modifier = modifier) {
        if (wideLayout) {
            val artworkSize = minOf(maxHeight - 24.dp, maxWidth * 0.58f, 520.dp).coerceAtLeast(160.dp)
            Row(
                modifier = Modifier.fillMaxSize().padding(horizontal = 44.dp, vertical = 20.dp),
                horizontalArrangement = Arrangement.spacedBy(36.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                PlayerArtwork(item = item, modifier = Modifier.size(artworkSize))
                ArtworkDetails(
                    item = item,
                    shuffleEnabled = shuffleEnabled,
                    repeatMode = repeatMode,
                    onCyclePlaybackMode = onCyclePlaybackMode,
                    onAddToPlaylist = onAddToPlaylist,
                    modifier = Modifier.weight(1f),
                )
            }
        } else {
            val compactLayout = maxHeight < 360.dp
            val artworkSize =
                if (compactLayout) {
                    minOf(
                        maxWidth - 48.dp,
                        (maxHeight - 116.dp).coerceAtLeast(72.dp),
                        260.dp,
                    )
                } else {
                    minOf(maxWidth - 32.dp, maxHeight * 0.66f, 560.dp).coerceAtLeast(140.dp)
                }
            Column(
                modifier =
                Modifier
                    .fillMaxSize()
                    .padding(horizontal = 24.dp, vertical = if (compactLayout) 4.dp else 8.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Spacer(modifier = Modifier.weight(1f))
                PlayerArtwork(item = item, modifier = Modifier.size(artworkSize))
                Spacer(modifier = Modifier.height(if (compactLayout) 8.dp else 18.dp))
                ArtworkDetails(
                    item = item,
                    shuffleEnabled = shuffleEnabled,
                    repeatMode = repeatMode,
                    onCyclePlaybackMode = onCyclePlaybackMode,
                    onAddToPlaylist = onAddToPlaylist,
                    modifier = Modifier.fillMaxWidth(),
                    centered = true,
                )
                Spacer(modifier = Modifier.weight(0.9f))
            }
        }
    }
}

@Composable
private fun ArtworkDetails(
    item: PlayerQueueItem,
    shuffleEnabled: Boolean,
    repeatMode: RepeatMode,
    onCyclePlaybackMode: () -> Unit,
    onAddToPlaylist: () -> Unit,
    modifier: Modifier = Modifier,
    centered: Boolean = false,
) {
    Column(modifier = modifier, horizontalAlignment = if (centered) Alignment.CenterHorizontally else Alignment.Start) {
        Text(
            text = item.artistNames.joinToString(" / ").ifBlank {
                stringResource(R.string.catalog_unknown_artist)
            },
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            color = PlayerSecondaryContent,
            style = MaterialTheme.typography.bodyLarge,
            textAlign = if (centered) TextAlign.Center else TextAlign.Start,
        )
        item.albumTitle?.takeIf(String::isNotBlank)?.let { album ->
            Text(
                text = album,
                modifier = Modifier.padding(top = 3.dp),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                color = PlayerMutedContent,
                style = MaterialTheme.typography.bodySmall,
                textAlign = if (centered) TextAlign.Center else TextAlign.Start,
            )
        }
        Row(
            modifier = Modifier.padding(top = 12.dp),
            horizontalArrangement = Arrangement.spacedBy(4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            PlayerPlaybackModeButton(
                shuffleEnabled = shuffleEnabled,
                repeatMode = repeatMode,
                onClick = onCyclePlaybackMode,
                showLabel = false,
            )
            IconButton(
                onClick = onAddToPlaylist,
                modifier =
                Modifier
                    .size(44.dp)
                    .clip(CircleShape)
                    .background(PlayerSubtleContent),
            ) {
                Icon(
                    imageVector = Icons.AutoMirrored.Outlined.PlaylistAdd,
                    contentDescription = stringResource(R.string.playlist_add_track),
                    tint = PlayerPrimaryContent,
                    modifier = Modifier.size(23.dp),
                )
            }
        }
    }
}

@Composable
internal fun PlayerArtwork(item: PlayerQueueItem, modifier: Modifier = Modifier) {
    MediaArtwork(
        url = item.artworkUrl,
        cacheKey = item.artworkCacheKey,
        contentDescription = item.title,
        fallbackImageRes = R.drawable.xymusic_compact,
        modifier =
        modifier
            .aspectRatio(1f),
        shape = RoundedCornerShape(18.dp),
        imageModifier = Modifier.testTag(PlayerTestTags.ArtworkImage),
        fallbackModifier = Modifier.testTag(PlayerTestTags.ArtworkPlaceholder),
        elevation = 24.dp,
    )
}

@Composable
internal fun EmptyPlayer(modifier: Modifier = Modifier) {
    Column(
        modifier = modifier.fillMaxWidth().padding(32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Box(
            modifier =
            Modifier
                .size(84.dp)
                .clip(RoundedCornerShape(24.dp))
                .background(PlayerSubtleContent),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = Icons.Outlined.MusicNote,
                contentDescription = null,
                modifier = Modifier.size(40.dp),
                tint = PlayerSecondaryContent,
            )
        }
        Text(
            text = stringResource(R.string.player_empty_title),
            modifier = Modifier.padding(top = 20.dp),
            textAlign = TextAlign.Center,
            style = MaterialTheme.typography.titleLarge,
            fontWeight = FontWeight.Bold,
            color = PlayerPrimaryContent,
        )
        Text(
            text = stringResource(R.string.player_empty_message),
            modifier = Modifier.padding(top = 8.dp),
            textAlign = TextAlign.Center,
            color = PlayerSecondaryContent,
            style = MaterialTheme.typography.bodyMedium,
        )
    }
}
