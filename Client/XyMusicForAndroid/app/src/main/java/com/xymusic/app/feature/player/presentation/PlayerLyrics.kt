package com.xymusic.app.feature.player.presentation

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.LinearOutSlowInEasing
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
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.TransformOrigin
import androidx.compose.ui.graphics.graphicsLayer
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
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.distinctUntilChanged

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
    val displayPosition = playbackPosition ?: rememberPlaybackPositionState(uiState.player)
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
    var previousWordByWordIndex by remember(uiState.player.currentItem?.trackId) { mutableStateOf<Int?>(null) }
    var outgoingWordByWordIndex by remember(uiState.player.currentItem?.trackId) { mutableStateOf<Int?>(null) }
    var observedPositionDiscontinuitySequence by
        remember(uiState.player.currentItem?.trackId) {
            mutableLongStateOf(uiState.player.positionDiscontinuitySequence)
        }
    val outgoingWordByWordAlpha = remember(uiState.player.currentItem?.trackId) { Animatable(0f) }
    val lyricLineStyle = lyricLineStyle(compact)

    LaunchedEffect(isDragged) {
        if (isDragged) autoFollow = false
    }
    LaunchedEffect(wordByWordHighlight?.first, uiState.player.positionDiscontinuitySequence) {
        val currentWordByWordIndex = wordByWordHighlight?.first
        val positionDiscontinuous =
            observedPositionDiscontinuitySequence != uiState.player.positionDiscontinuitySequence
        val outgoingIndex =
            outgoingLyricHighlightIndex(
                previousIndex = previousWordByWordIndex,
                currentIndex = currentWordByWordIndex,
                positionDiscontinuous = positionDiscontinuous,
            )
        previousWordByWordIndex = currentWordByWordIndex
        observedPositionDiscontinuitySequence = uiState.player.positionDiscontinuitySequence
        if (outgoingIndex == null) {
            outgoingWordByWordIndex = null
            outgoingWordByWordAlpha.snapTo(0f)
            return@LaunchedEffect
        }

        outgoingWordByWordIndex = outgoingIndex
        outgoingWordByWordAlpha.snapTo(1f)
        outgoingWordByWordAlpha.animateTo(
            targetValue = 0f,
            animationSpec =
            tween(
                durationMillis = LYRIC_OUTGOING_HIGHLIGHT_DURATION_MILLIS,
                easing = LinearOutSlowInEasing,
            ),
        )
        if (outgoingWordByWordIndex == outgoingIndex) outgoingWordByWordIndex = null
    }
    BoxWithConstraints(modifier = modifier.fillMaxSize()) {
        LaunchedEffect(
            autoFollow,
            uiState.player.currentItem?.trackId,
            uiState.player.positionDiscontinuitySequence,
            uiState.lyrics,
            centerActiveLine,
            maxHeight,
        ) {
            if (!autoFollow) return@LaunchedEffect
            var previousLyricIndex: Int? = null
            snapshotFlow { currentLyricIndex }
                .distinctUntilChanged()
                .collectLatest { lyricIndex ->
                    if (lyricIndex < 0) {
                        previousLyricIndex = null
                        return@collectLatest
                    }
                    val targetIndex =
                        if (centerActiveLine) lyricIndex else (lyricIndex - 2).coerceAtLeast(0)
                    val scrollMode =
                        lyricFollowScrollMode(
                            previousLyricIndex = previousLyricIndex,
                            lyricIndex = lyricIndex,
                        )
                    previousLyricIndex = lyricIndex
                    listState.followLyricLine(
                        index = targetIndex,
                        centerActiveLine = centerActiveLine,
                        scrollMode = scrollMode,
                    )
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
                    val wordByWordOutgoing = outgoingWordByWordIndex == index
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
                        animationSpec = lyricHighlightTransitionSpec(),
                        label = "lyricColor",
                    )
                    val lineScale by animateFloatAsState(
                        targetValue = if (active) lyricLineStyle.activeScale else 1f,
                        animationSpec = lyricHighlightTransitionSpec(),
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
                        (lineEndTimeMs != null || wordByWordOutgoing)
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
                            highlightProgressOverride =
                            if (wordByWordOutgoing) {
                                WordByWordHighlightProgress(
                                    completedCount = line.highlightEndOffsets.size,
                                    currentFraction = 0f,
                                )
                            } else {
                                null
                            },
                            highlightAlpha = if (wordByWordOutgoing) outgoingWordByWordAlpha.value else 1f,
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

internal enum class LyricFollowScrollMode {
    Snap,
    Animate,
}

internal fun lyricFollowScrollMode(previousLyricIndex: Int?, lyricIndex: Int): LyricFollowScrollMode = if (
    previousLyricIndex == null ||
    abs(lyricIndex - previousLyricIndex) > 1
) {
    LyricFollowScrollMode.Snap
} else {
    LyricFollowScrollMode.Animate
}

internal fun outgoingLyricHighlightIndex(
    previousIndex: Int?,
    currentIndex: Int?,
    positionDiscontinuous: Boolean,
): Int? = previousIndex?.takeIf { previous ->
    !positionDiscontinuous && currentIndex == previous + 1
}

private suspend fun LazyListState.followLyricLine(
    index: Int,
    centerActiveLine: Boolean,
    scrollMode: LyricFollowScrollMode,
) {
    when (scrollMode) {
        LyricFollowScrollMode.Snap ->
            if (centerActiveLine) {
                snapScrollToCenteredItem(index)
            } else {
                scrollToItem(index)
            }
        LyricFollowScrollMode.Animate ->
            if (centerActiveLine) {
                animateScrollToCenteredItem(index)
            } else {
                animateScrollToAlignedItem(index)
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

private suspend fun LazyListState.animateScrollToCenteredItem(index: Int) {
    if (centeredItemDelta(index) == null) {
        animateScrollToItem(index)
        withFrameNanos {}
    }
    val delta = centeredItemDelta(index) ?: return
    if (abs(delta) > 0.5f) {
        animateScrollBy(
            value = delta,
            animationSpec = lyricScrollTransitionSpec(),
        )
    }
    correctCenteredItem(index)
}

private suspend fun LazyListState.animateScrollToAlignedItem(index: Int) {
    val delta = alignedItemDelta(index) ?: run {
        animateScrollToItem(index)
        return
    }
    if (abs(delta) > 0.5f) {
        animateScrollBy(
            value = delta,
            animationSpec = lyricScrollTransitionSpec(),
        )
    }
}

private suspend fun LazyListState.correctCenteredItem(index: Int) {
    withFrameNanos {}
    val residual = centeredItemDelta(index) ?: return
    if (abs(residual) > 0.5f) scrollBy(residual)
}

private fun LazyListState.centeredItemDelta(index: Int): Float? {
    val item = layoutInfo.visibleItemsInfo.firstOrNull { it.index == index } ?: return null
    val viewportCenter = (layoutInfo.viewportStartOffset + layoutInfo.viewportEndOffset) / 2f
    val itemCenter = item.offset + item.size / 2f
    return itemCenter - viewportCenter
}

private fun LazyListState.alignedItemDelta(index: Int): Float? {
    val item = layoutInfo.visibleItemsInfo.firstOrNull { it.index == index } ?: return null
    return (item.offset - layoutInfo.viewportStartOffset).toFloat()
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

private fun <T> lyricHighlightTransitionSpec() = tween<T>(
    durationMillis = LYRIC_HIGHLIGHT_TRANSITION_DURATION_MILLIS,
    easing = FastOutSlowInEasing,
)

private fun lyricScrollTransitionSpec() = tween<Float>(
    durationMillis = LYRIC_SCROLL_TRANSITION_DURATION_MILLIS,
    easing = LinearOutSlowInEasing,
)

private data class LyricLineStyle(val fontSize: TextUnit, val lineHeight: TextUnit, val activeScale: Float)

private const val LYRIC_HIGHLIGHT_TRANSITION_DURATION_MILLIS = 200
private const val LYRIC_SCROLL_TRANSITION_DURATION_MILLIS = 200
private const val LYRIC_OUTGOING_HIGHLIGHT_DURATION_MILLIS = 140
