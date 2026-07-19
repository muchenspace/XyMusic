package com.xymusic.app.feature.player.presentation

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.IconButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.TransformOrigin
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import com.xymusic.app.core.ui.component.MediaArtwork
import com.xymusic.app.core.ui.component.XyMarqueeText
import com.xymusic.app.feature.player.domain.model.PlaybackState
import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
import com.xymusic.app.feature.player.domain.model.PlayerState

@Composable
fun PlayerMiniBar(
    uiState: PlayerUiState,
    onOpenPlayer: () -> Unit,
    onTogglePlayback: () -> Unit,
    onNext: () -> Unit,
    compact: Boolean = false,
    modifier: Modifier = Modifier,
) {
    val current = uiState.player.currentItem ?: return
    val colorScheme = MaterialTheme.colorScheme
    val playbackPosition = rememberSmoothedPlaybackPositionState(uiState.player)
    val metrics = playerMiniBarMetrics(compact)

    Box(
        modifier =
        modifier
            .fillMaxWidth()
            .height(metrics.barHeight)
            .background(colorScheme.surface)
            .testTag(PlayerTestTags.MiniBar)
            .clickable(role = Role.Button, onClick = onOpenPlayer),
    ) {
        MiniBarTopDivider(
            modifier = Modifier.align(Alignment.TopCenter),
        )
        PlayerMiniBarContent(
            item = current,
            player = uiState.player,
            metrics = metrics,
            onOpenPlayer = onOpenPlayer,
            onTogglePlayback = onTogglePlayback,
            onNext = onNext,
        )
        MiniBarProgress(
            positionMs = playbackPosition.value,
            durationMs = uiState.player.durationMs,
            modifier = Modifier.align(Alignment.BottomCenter),
        )
    }
}

@Composable
private fun PlayerMiniBarContent(
    item: PlayerQueueItem,
    player: PlayerState,
    metrics: PlayerMiniBarMetrics,
    onOpenPlayer: () -> Unit,
    onTogglePlayback: () -> Unit,
    onNext: () -> Unit,
) {
    Row(
        modifier =
        Modifier
            .fillMaxSize()
            .padding(
                start = 8.dp,
                end = 2.dp,
                top = metrics.contentTopPadding,
                bottom = metrics.contentBottomPadding,
            ),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        MediaArtwork(
            url = item.artworkUrl,
            cacheKey = item.artworkCacheKey,
            contentDescription = null,
            fallbackImageRes = R.drawable.xymusic,
            modifier =
            Modifier
                .size(metrics.artworkSize)
                .clip(RoundedCornerShape(metrics.artworkCornerRadius)),
            imageModifier = Modifier.testTag(PlayerTestTags.ArtworkImage),
            fallbackModifier = Modifier.testTag(PlayerTestTags.ArtworkPlaceholder),
        )
        Spacer(modifier = Modifier.width(metrics.artworkGap))
        MiniBarTrackInfo(
            item = item,
            onOpenPlayer = onOpenPlayer,
            modifier = Modifier.weight(1f),
        )
        MiniBarPlaybackButton(
            player = player,
            metrics = metrics,
            onClick = onTogglePlayback,
        )
        MiniBarNextButton(metrics = metrics, onClick = onNext)
    }
}

@Composable
private fun MiniBarTrackInfo(item: PlayerQueueItem, onOpenPlayer: () -> Unit, modifier: Modifier = Modifier) {
    Column(
        modifier =
        modifier
            .testTag(PlayerTestTags.OpenPlayer)
            .clickable(role = Role.Button, onClick = onOpenPlayer),
    ) {
        XyMarqueeText(
            text = item.title,
            style = MaterialTheme.typography.titleSmall.copy(fontWeight = FontWeight.SemiBold),
            color = MaterialTheme.colorScheme.onSurface,
        )
        Text(
            text =
            item.artistNames.joinToString(" / ").ifBlank {
                stringResource(R.string.catalog_unknown_artist)
            },
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.bodySmall,
        )
    }
}

@Composable
private fun MiniBarPlaybackButton(player: PlayerState, metrics: PlayerMiniBarMetrics, onClick: () -> Unit) {
    IconButton(
        onClick = onClick,
        modifier = Modifier.size(metrics.controlSize).testTag(PlayerTestTags.TogglePlayback),
        colors = IconButtonDefaults.iconButtonColors(contentColor = MaterialTheme.colorScheme.onSurface),
    ) {
        if (player.playbackState == PlaybackState.BUFFERING) {
            CircularProgressIndicator(
                modifier = Modifier.size(21.dp),
                strokeWidth = 2.dp,
                color = MaterialTheme.colorScheme.onSurface,
            )
        } else {
            Icon(
                imageVector = if (player.isPlaying) Icons.Default.Pause else Icons.Default.PlayArrow,
                contentDescription =
                stringResource(
                    if (player.isPlaying) R.string.player_pause else R.string.player_play,
                ),
                modifier = Modifier.size(metrics.toggleIconSize),
            )
        }
    }
}

@Composable
private fun MiniBarNextButton(metrics: PlayerMiniBarMetrics, onClick: () -> Unit) {
    IconButton(
        onClick = onClick,
        modifier = Modifier.size(metrics.controlSize).testTag(PlayerTestTags.Next),
        colors = IconButtonDefaults.iconButtonColors(contentColor = MaterialTheme.colorScheme.onSurface),
    ) {
        Icon(
            imageVector = Icons.Default.SkipNext,
            contentDescription = stringResource(R.string.player_next),
            modifier = Modifier.size(metrics.nextIconSize),
        )
    }
}

@Composable
private fun MiniBarTopDivider(modifier: Modifier = Modifier) {
    Box(
        modifier =
        modifier
            .fillMaxWidth()
            .height(0.5.dp)
            .background(MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.65f)),
    )
}

@Composable
private fun MiniBarProgress(positionMs: Float, durationMs: Long, modifier: Modifier = Modifier) {
    Box(
        modifier =
        modifier
            .fillMaxWidth()
            .height(2.dp)
            .background(MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f)),
    ) {
        Box(
            modifier =
            Modifier
                .fillMaxSize()
                .graphicsLayer {
                    transformOrigin = TransformOrigin(0f, 0.5f)
                    scaleX = normalizedPlaybackProgress(positionMs = positionMs, durationMs = durationMs)
                }.background(MaterialTheme.colorScheme.primary),
        )
    }
}

private fun playerMiniBarMetrics(compact: Boolean): PlayerMiniBarMetrics = if (compact) {
    PlayerMiniBarMetrics(
        barHeight = 52.dp,
        artworkSize = 40.dp,
        artworkCornerRadius = 7.dp,
        artworkGap = 8.dp,
        contentTopPadding = 4.dp,
        contentBottomPadding = 6.dp,
        controlSize = 40.dp,
        toggleIconSize = 26.dp,
        nextIconSize = 25.dp,
    )
} else {
    PlayerMiniBarMetrics(
        barHeight = 64.dp,
        artworkSize = 48.dp,
        artworkCornerRadius = 8.dp,
        artworkGap = 10.dp,
        contentTopPadding = 6.dp,
        contentBottomPadding = 8.dp,
        controlSize = 44.dp,
        toggleIconSize = 28.dp,
        nextIconSize = 27.dp,
    )
}

private data class PlayerMiniBarMetrics(
    val barHeight: Dp,
    val artworkSize: Dp,
    val artworkCornerRadius: Dp,
    val artworkGap: Dp,
    val contentTopPadding: Dp,
    val contentBottomPadding: Dp,
    val controlSize: Dp,
    val toggleIconSize: Dp,
    val nextIconSize: Dp,
)
