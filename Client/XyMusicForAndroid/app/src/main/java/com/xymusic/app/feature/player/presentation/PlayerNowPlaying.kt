@file:OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)

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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.PlaylistAdd
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material.icons.outlined.MusicNote
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
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import com.xymusic.app.core.ui.component.MediaArtwork
import com.xymusic.app.core.ui.component.XyMarqueeText
import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
import com.xymusic.app.feature.player.domain.model.RepeatMode

@Composable
internal fun NowPlayingContent(
    item: PlayerQueueItem,
    isFavorite: Boolean,
    shuffleEnabled: Boolean,
    repeatMode: RepeatMode,
    onToggleFavorite: () -> Unit,
    onCyclePlaybackMode: () -> Unit,
    onAddToPlaylist: () -> Unit,
    wideLayout: Boolean,
    modifier: Modifier = Modifier,
) {
    BoxWithConstraints(modifier = modifier) {
        if (wideLayout) {
            val artworkSize = minOf(maxHeight - 16.dp, maxWidth * 0.38f, 320.dp).coerceAtLeast(96.dp)
            Row(
                modifier = Modifier.fillMaxSize().padding(horizontal = 28.dp, vertical = 8.dp),
                horizontalArrangement = Arrangement.spacedBy(28.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                PlayerArtwork(item = item, modifier = Modifier.size(artworkSize))
                TrackMetadata(
                    item = item,
                    isFavorite = isFavorite,
                    shuffleEnabled = shuffleEnabled,
                    repeatMode = repeatMode,
                    onToggleFavorite = onToggleFavorite,
                    onCyclePlaybackMode = onCyclePlaybackMode,
                    onAddToPlaylist = onAddToPlaylist,
                    modifier = Modifier.weight(1f),
                )
            }
        } else {
            val artworkSize = minOf(maxWidth - 48.dp, maxHeight * 0.70f, 390.dp).coerceAtLeast(120.dp)
            Column(
                modifier = Modifier.fillMaxSize().padding(horizontal = 24.dp, vertical = 6.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.Center,
            ) {
                PlayerArtwork(item = item, modifier = Modifier.size(artworkSize))
                Spacer(modifier = Modifier.height(18.dp))
                TrackMetadata(
                    item = item,
                    isFavorite = isFavorite,
                    shuffleEnabled = shuffleEnabled,
                    repeatMode = repeatMode,
                    onToggleFavorite = onToggleFavorite,
                    onCyclePlaybackMode = onCyclePlaybackMode,
                    onAddToPlaylist = onAddToPlaylist,
                    modifier = Modifier.fillMaxWidth(),
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
        fallbackImageRes = R.drawable.xymusic,
        modifier =
        modifier
            .aspectRatio(1f),
        shape = RoundedCornerShape(12.dp),
        imageModifier = Modifier.testTag(PlayerTestTags.ArtworkImage),
        fallbackModifier = Modifier.testTag(PlayerTestTags.ArtworkPlaceholder),
        elevation = 18.dp,
    )
}

@Composable
internal fun TrackMetadata(
    item: PlayerQueueItem,
    isFavorite: Boolean,
    shuffleEnabled: Boolean,
    repeatMode: RepeatMode,
    onToggleFavorite: () -> Unit,
    onCyclePlaybackMode: () -> Unit,
    onAddToPlaylist: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var moreExpanded by remember { mutableStateOf(false) }
    Column(modifier = modifier) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            XyMarqueeText(
                text = item.title,
                modifier = Modifier.weight(1f),
                style = MaterialTheme.typography.headlineMedium.copy(fontWeight = FontWeight.SemiBold),
                color = PlayerPrimaryContent,
                maxLines = 1,
            )
            IconButton(onClick = onToggleFavorite, modifier = Modifier.size(44.dp)) {
                Icon(
                    imageVector = if (isFavorite) Icons.Default.Favorite else Icons.Outlined.FavoriteBorder,
                    contentDescription = stringResource(
                        if (isFavorite) R.string.library_remove_favorite else R.string.library_add_favorite,
                    ),
                    tint = PlayerPrimaryContent,
                    modifier = Modifier.size(23.dp),
                )
            }
            PlayerPlaybackModeButton(
                shuffleEnabled = shuffleEnabled,
                repeatMode = repeatMode,
                onClick = onCyclePlaybackMode,
                showLabel = false,
            )
            Box {
                IconButton(onClick = { moreExpanded = true }, modifier = Modifier.size(44.dp)) {
                    Icon(
                        Icons.Default.MoreVert,
                        contentDescription = stringResource(R.string.common_more_actions),
                        tint = PlayerPrimaryContent,
                        modifier = Modifier.size(26.dp),
                    )
                }
                DropdownMenu(
                    expanded = moreExpanded,
                    onDismissRequest = { moreExpanded = false },
                    modifier = Modifier.background(MaterialTheme.colorScheme.surfaceContainerHigh),
                ) {
                    DropdownMenuItem(
                        text = {
                            Text(
                                stringResource(
                                    if (isFavorite) R.string.library_remove_favorite else R.string.library_add_favorite,
                                ),
                                color = PlayerPrimaryContent,
                            )
                        },
                        leadingIcon = {
                            Icon(
                                if (isFavorite) Icons.Default.Favorite else Icons.Outlined.FavoriteBorder,
                                contentDescription = null,
                                tint = PlayerPrimaryContent,
                            )
                        },
                        onClick = {
                            moreExpanded = false
                            onToggleFavorite()
                        },
                    )
                    DropdownMenuItem(
                        text = {
                            Text(stringResource(R.string.playlist_add_track), color = PlayerPrimaryContent)
                        },
                        leadingIcon = {
                            Icon(
                                Icons.AutoMirrored.Outlined.PlaylistAdd,
                                contentDescription = null,
                                tint = PlayerPrimaryContent,
                            )
                        },
                        onClick = {
                            moreExpanded = false
                            onAddToPlaylist()
                        },
                    )
                }
            }
        }
        Text(
            text =
            item.artistNames.joinToString(" / ").ifBlank {
                stringResource(R.string.catalog_unknown_artist)
            },
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            color = PlayerSecondaryContent,
            style = MaterialTheme.typography.bodyLarge,
            fontWeight = FontWeight.Medium,
        )
        item.albumTitle?.takeIf(String::isNotBlank)?.let { album ->
            Text(
                text = album,
                modifier = Modifier.padding(top = 2.dp),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                color = PlayerMutedContent,
                style = MaterialTheme.typography.bodySmall,
            )
        }
    }
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
                .size(80.dp)
                .clip(RoundedCornerShape(24.dp))
                .background(PlayerSubtleContent),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                Icons.Outlined.MusicNote,
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
