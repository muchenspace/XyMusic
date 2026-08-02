package com.xymusic.app.feature.player.presentation

import androidx.compose.foundation.Canvas
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
import androidx.compose.runtime.State
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
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

internal val PlayerMiniBarHeight = 64.dp
internal val CompactPlayerMiniBarHeight = 52.dp

@Composable
fun PlayerMiniBar(
    uiState: PlayerUiState,
    onOpenPlayer: () -> Unit,
    onTogglePlayback: () -> Unit,
    onNext: () -> Unit,
    modifier: Modifier = Modifier,
    playbackPosition: State<Float>? = null,
    compact: Boolean = false,
) {
    val current = uiState.player.currentItem ?: return
    val colorScheme = MaterialTheme.colorScheme
    val displayedPlaybackPosition =
        playbackPosition ?: rememberPlaybackPositionSnapshotState(uiState.player)
    val metrics = playerMiniBarMetrics(compact)
    val artistLine =
        remember(current.queueItemId, current.artistNames) {
            current.artistNames.joinToString(" / ")
        }.ifBlank {
            stringResource(R.string.catalog_unknown_artist)
        }

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
            title = current.title,
            artistLine = artistLine,
            artworkUrl = current.artworkUrl,
            artworkCacheKey = current.artworkCacheKey,
            isPlaying = uiState.player.isPlaying,
            isBuffering = uiState.player.playbackState == PlaybackState.BUFFERING,
            metrics = metrics,
            onOpenPlayer = onOpenPlayer,
            onTogglePlayback = onTogglePlayback,
            onNext = onNext,
        )
        MiniBarProgress(
            positionMs = displayedPlaybackPosition,
            durationMs = uiState.player.durationMs,
            modifier = Modifier.align(Alignment.BottomCenter),
        )
    }
}

@Composable
private fun PlayerMiniBarContent(
    title: String,
    artistLine: String,
    artworkUrl: String?,
    artworkCacheKey: String?,
    isPlaying: Boolean,
    isBuffering: Boolean,
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
            url = artworkUrl,
            cacheKey = artworkCacheKey,
            contentDescription = null,
            fallbackImageRes = R.drawable.xymusic_compact,
            modifier =
            Modifier
                .size(metrics.artworkSize)
                .clip(RoundedCornerShape(metrics.artworkCornerRadius)),
            imageModifier = Modifier.testTag(PlayerTestTags.ArtworkImage),
            fallbackModifier = Modifier.testTag(PlayerTestTags.ArtworkPlaceholder),
        )
        Spacer(modifier = Modifier.width(metrics.artworkGap))
        MiniBarTrackInfo(
            title = title,
            artistLine = artistLine,
            onOpenPlayer = onOpenPlayer,
            modifier = Modifier.weight(1f),
        )
        MiniBarPlaybackButton(
            isPlaying = isPlaying,
            isBuffering = isBuffering,
            metrics = metrics,
            onClick = onTogglePlayback,
        )
        MiniBarNextButton(metrics = metrics, onClick = onNext)
    }
}

@Composable
private fun MiniBarTrackInfo(
    title: String,
    artistLine: String,
    onOpenPlayer: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier =
        modifier
            .testTag(PlayerTestTags.OpenPlayer)
            .clickable(role = Role.Button, onClick = onOpenPlayer),
    ) {
        XyMarqueeText(
            text = title,
            style = MaterialTheme.typography.titleSmall.copy(fontWeight = FontWeight.SemiBold),
            color = MaterialTheme.colorScheme.onSurface,
        )
        Text(
            text =
            artistLine,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.bodySmall,
        )
    }
}

@Composable
private fun MiniBarPlaybackButton(
    isPlaying: Boolean,
    isBuffering: Boolean,
    metrics: PlayerMiniBarMetrics,
    onClick: () -> Unit,
) {
    IconButton(
        onClick = onClick,
        modifier = Modifier.size(metrics.controlSize).testTag(PlayerTestTags.TogglePlayback),
        colors = IconButtonDefaults.iconButtonColors(contentColor = MaterialTheme.colorScheme.onSurface),
    ) {
        if (isBuffering) {
            CircularProgressIndicator(
                modifier = Modifier.size(21.dp),
                strokeWidth = 2.dp,
                color = MaterialTheme.colorScheme.onSurface,
            )
        } else {
            Icon(
                imageVector = if (isPlaying) Icons.Default.Pause else Icons.Default.PlayArrow,
                contentDescription =
                stringResource(
                    if (isPlaying) R.string.player_pause else R.string.player_play,
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
private fun MiniBarProgress(positionMs: State<Float>, durationMs: Long, modifier: Modifier = Modifier) {
    val trackColor = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f)
    val progressColor = MaterialTheme.colorScheme.primary
    Canvas(
        modifier =
        modifier
            .fillMaxWidth()
            .height(2.dp)
            .background(trackColor),
    ) {
        val progress = normalizedPlaybackProgress(positionMs = positionMs.value, durationMs = durationMs)
        if (progress > 0f) {
            drawRect(
                color = progressColor,
                size = size.copy(width = size.width * progress),
            )
        }
    }
}

private fun playerMiniBarMetrics(compact: Boolean): PlayerMiniBarMetrics = if (compact) {
    PlayerMiniBarMetrics(
        barHeight = CompactPlayerMiniBarHeight,
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
        barHeight = PlayerMiniBarHeight,
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
