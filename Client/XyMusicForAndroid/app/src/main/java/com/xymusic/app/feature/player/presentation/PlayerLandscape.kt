package com.xymusic.app.feature.player.presentation

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import com.xymusic.app.feature.player.domain.model.PlayerQueueItem

@Composable
internal fun LandscapeNowPlayingContent(
    item: PlayerQueueItem,
    uiState: PlayerUiState,
    onSeek: (Long) -> Unit,
    onTogglePlayback: () -> Unit,
    onPrevious: () -> Unit,
    onNext: () -> Unit,
    leftPaneModifier: Modifier = Modifier,
    modifier: Modifier = Modifier,
) {
    Box(modifier = modifier) {
        Row(
            modifier = Modifier.fillMaxSize().padding(horizontal = 28.dp, vertical = 8.dp),
            horizontalArrangement = Arrangement.spacedBy(28.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            BoxWithConstraints(
                modifier =
                Modifier
                    .weight(0.9f)
                    .fillMaxHeight()
                    .testTag(PlayerTestTags.LandscapeArtworkPane)
                    .then(leftPaneModifier),
                contentAlignment = Alignment.CenterStart,
            ) {
                val compactLayout = maxHeight < 360.dp
                val controlsHeight = if (compactLayout) 52.dp else 60.dp
                val sectionSpacing = if (compactLayout) 8.dp else 10.dp
                val availableArtworkHeight =
                    (maxHeight - controlsHeight - sectionSpacing)
                        .coerceAtLeast(0.dp)
                val artworkSize = minOf(maxWidth, availableArtworkHeight, 400.dp)
                Column(
                    modifier = Modifier.width(artworkSize),
                    horizontalAlignment = Alignment.Start,
                ) {
                    PlayerArtwork(
                        item = item,
                        modifier = Modifier.size(artworkSize).testTag(PlayerTestTags.LandscapeArtwork),
                    )
                    Spacer(modifier = Modifier.height(sectionSpacing))
                    LandscapeTransportControls(
                        player = uiState.player,
                        onTogglePlayback = onTogglePlayback,
                        onPrevious = onPrevious,
                        onNext = onNext,
                        compact = compactLayout,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }
            Box(
                modifier =
                Modifier
                    .weight(1.1f)
                    .fillMaxHeight()
                    .testTag(PlayerTestTags.LandscapeLyricsPane),
            ) {
                LyricsContent(
                    uiState = uiState,
                    onSeek = onSeek,
                    compact = true,
                    centerActiveLine = true,
                    modifier = Modifier.fillMaxSize(),
                )
            }
        }
    }
}
