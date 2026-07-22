package com.xymusic.app.feature.player.presentation

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.animateScrollBy
import androidx.compose.foundation.gestures.scrollBy
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsDraggedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.GraphicEq
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.State
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.TransformOrigin
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.xymusic.app.R
import kotlin.math.abs
import kotlin.math.roundToInt

@Composable
internal fun LyricsContent(
    uiState: PlayerUiState,
    onSeek: (Long) -> Unit,
    modifier: Modifier = Modifier,
    compact: Boolean = false,
    centerActiveLine: Boolean = false,
    playbackPosition: State<Float>? = null,
) {
    val listState = rememberLazyListState()
    val isDragged by listState.interactionSource.collectIsDraggedAsState()
    val displayPosition = playbackPosition ?: rememberSmoothedPlaybackPositionState(uiState.player)
    val currentLyricIndex by remember(
        uiState.lyrics,
        uiState.synchronizedLyrics,
        displayPosition,
    ) {
        derivedStateOf {
            if (uiState.synchronizedLyrics) {
                playbackLyricIndex(uiState.lyrics, displayPosition.value.toLong())
            } else {
                -1
            }
        }
    }
    val wordByWordHighlight by remember(
        uiState.lyrics,
        uiState.synchronizedLyrics,
        uiState.wordByWordLyricsEnabled,
        uiState.player.durationMs,
        displayPosition,
    ) {
        derivedStateOf {
            if (!uiState.synchronizedLyrics || !uiState.wordByWordLyricsEnabled) {
                null
            } else {
                estimatedWordByWordLyricProgress(
                    lines = uiState.lyrics,
                    positionMs = displayPosition.value.toLong(),
                    durationMs = uiState.player.durationMs,
                )?.let { progress ->
                    progress.lineIndex to progress.lineEndTimeMs
                }
            }
        }
    }
    var autoFollow by rememberSaveable(uiState.player.currentItem?.trackId) { mutableStateOf(true) }
    var hasCenteredActiveLine by remember(uiState.player.currentItem?.trackId) { mutableStateOf(false) }
    val lyricLineStyle = lyricLineStyle(compact)
    val density = LocalDensity.current
    val approximateCenterOffset =
        with(density) {
            (lyricLineStyle.lineHeight.toPx() / 2f).roundToInt()
        }

    LaunchedEffect(isDragged) {
        if (isDragged) autoFollow = false
    }
    BoxWithConstraints(modifier = modifier.fillMaxSize()) {
        LaunchedEffect(currentLyricIndex, autoFollow, centerActiveLine, maxHeight) {
            if (autoFollow && currentLyricIndex >= 0) {
                if (centerActiveLine) {
                    if (hasCenteredActiveLine) {
                        listState.animateScrollToCenteredItem(
                            index = currentLyricIndex,
                            approximateCenterOffset = approximateCenterOffset,
                        )
                    } else {
                        hasCenteredActiveLine = true
                        listState.snapScrollToCenteredItem(currentLyricIndex)
                    }
                } else {
                    listState.animateScrollToItem((currentLyricIndex - 2).coerceAtLeast(0))
                }
            }
        }
        if (uiState.lyrics.isEmpty()) {
            Text(
                text = stringResource(R.string.player_no_lyrics),
                modifier = Modifier.align(Alignment.Center),
                color = PlayerSecondaryContent,
                style = MaterialTheme.typography.bodyLarge,
            )
        } else {
            LazyColumn(
                state = listState,
                modifier = Modifier.fillMaxSize(),
                contentPadding =
                PaddingValues(
                    horizontal = if (compact) 0.dp else 28.dp,
                    vertical =
                    when {
                        centerActiveLine -> maxHeight / 2
                        compact -> 24.dp
                        else -> 46.dp
                    },
                ),
                verticalArrangement = Arrangement.spacedBy(if (compact) 14.dp else 20.dp),
            ) {
                itemsIndexed(
                    items = uiState.lyrics,
                    key = { index, line -> "${line.timeMs ?: "plain"}:$index" },
                    contentType = { _, _ -> "lyric-line" },
                ) { index, line ->
                    val wordByWordActive = wordByWordHighlight?.first == index
                    val wordByWordAvailable = line.highlightEndOffsets.isNotEmpty()
                    val active =
                        uiState.synchronizedLyrics &&
                            if (uiState.wordByWordLyricsEnabled && wordByWordAvailable) {
                                wordByWordActive
                            } else {
                                index == currentLyricIndex
                            }
                    val targetColor =
                        when {
                            !uiState.synchronizedLyrics -> PlayerPrimaryContent.copy(alpha = 0.88f)
                            uiState.wordByWordLyricsEnabled && wordByWordAvailable -> PlayerMutedContent
                            active -> PlayerPrimaryContent
                            else -> PlayerMutedContent
                        }
                    val lineColor by animateColorAsState(
                        targetValue = targetColor,
                        animationSpec = lyricTransitionSpec(),
                        label = "lyricColor",
                    )
                    val lineScale by animateFloatAsState(
                        targetValue = if (active) lyricLineStyle.activeScale else 1f,
                        animationSpec = lyricTransitionSpec(),
                        label = "lyricScale",
                    )
                    val interactionSource = remember { MutableInteractionSource() }
                    val lineModifier =
                        Modifier
                            .fillMaxWidth()
                            .graphicsLayer {
                                transformOrigin = TransformOrigin(0f, 0.5f)
                                scaleX = lineScale
                                scaleY = lineScale
                            }
                            .clickable(
                                interactionSource = interactionSource,
                                indication = null,
                                enabled = uiState.synchronizedLyrics && line.timeMs != null,
                                role = Role.Button,
                                onClick = {
                                    line.timeMs?.let(onSeek)
                                    autoFollow = true
                                },
                            ).semantics {
                                if (uiState.synchronizedLyrics) selected = active
                            }
                    val lineTextStyle =
                        LocalTextStyle.current.merge(
                            TextStyle(
                                fontSize = lyricLineStyle.fontSize,
                                lineHeight = lyricLineStyle.lineHeight,
                                fontWeight = FontWeight.SemiBold,
                                letterSpacing = 0.sp,
                            ),
                        )
                    val lineStartTimeMs = line.timeMs
                    val lineEndTimeMs = if (wordByWordActive) wordByWordHighlight?.second else null
                    if (
                        uiState.wordByWordLyricsEnabled &&
                        wordByWordAvailable &&
                        lineStartTimeMs != null &&
                        lineEndTimeMs != null
                    ) {
                        WordByWordLyricText(
                            text = line.text,
                            highlightEndOffsets = line.highlightEndOffsets,
                            playbackPosition = displayPosition,
                            lineStartTimeMs = lineStartTimeMs,
                            lineEndTimeMs = lineEndTimeMs,
                            modifier = lineModifier,
                            baseColor = lineColor,
                            highlightColor = PlayerPrimaryContent,
                            style = lineTextStyle,
                        )
                    } else {
                        Text(
                            text = line.text,
                            modifier = lineModifier,
                            color = lineColor,
                            style = lineTextStyle,
                        )
                    }
                }
            }
        }
        if (!autoFollow && uiState.synchronizedLyrics) {
            FilledTonalButton(
                onClick = { autoFollow = true },
                modifier =
                Modifier
                    .align(Alignment.BottomCenter)
                    .padding(if (compact) 8.dp else 12.dp),
                colors =
                ButtonDefaults.filledTonalButtonColors(
                    containerColor = PlayerPrimaryContent.copy(alpha = 0.16f),
                    contentColor = PlayerPrimaryContent,
                ),
            ) {
                Icon(Icons.Default.GraphicEq, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(
                    text = stringResource(R.string.player_resume_lyrics_follow),
                    modifier = Modifier.padding(start = 6.dp),
                )
            }
        }
    }
}

private suspend fun LazyListState.snapScrollToCenteredItem(index: Int) {
    scrollToItem(index)
    withFrameNanos {}
    centeredItemDelta(index)?.let { scrollBy(it) }
    withFrameNanos {}
    val residual = centeredItemDelta(index) ?: return
    if (abs(residual) > 0.5f) scrollBy(residual)
}

private suspend fun LazyListState.animateScrollToCenteredItem(index: Int, approximateCenterOffset: Int) {
    val delta = centeredItemDelta(index)
    if (delta == null) {
        animateScrollToItem(index = index, scrollOffset = approximateCenterOffset)
    } else if (abs(delta) > 0.5f) {
        animateScrollBy(
            value = delta,
            animationSpec = lyricTransitionSpec(),
        )
    }
}

private fun LazyListState.centeredItemDelta(index: Int): Float? {
    val item = layoutInfo.visibleItemsInfo.firstOrNull { it.index == index } ?: return null
    val viewportCenter = (layoutInfo.viewportStartOffset + layoutInfo.viewportEndOffset) / 2f
    val itemCenter = item.offset + item.size / 2f
    return itemCenter - viewportCenter
}

private fun lyricLineStyle(compact: Boolean): LyricLineStyle = if (compact) {
    LyricLineStyle(
        fontSize = 24.sp,
        lineHeight = 34.sp,
        activeScale = 1.1f,
    )
} else {
    LyricLineStyle(
        fontSize = 28.sp,
        lineHeight = 39.sp,
        activeScale = 1.08f,
    )
}

private fun <T> lyricTransitionSpec() = tween<T>(
    durationMillis = LYRIC_TRANSITION_DURATION_MILLIS,
    easing = FastOutSlowInEasing,
)

private data class LyricLineStyle(val fontSize: TextUnit, val lineHeight: TextUnit, val activeScale: Float)

private const val LYRIC_TRANSITION_DURATION_MILLIS = 420
