package com.xymusic.app.feature.player.presentation

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.AnimationVector1D
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.FiniteAnimationSpec
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.animateScrollBy
import androidx.compose.foundation.gestures.scrollBy
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsDraggedAsState
import androidx.compose.foundation.interaction.collectIsPressedAsState
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
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.TransformOrigin
import androidx.compose.ui.graphics.drawscope.ContentDrawScope
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextLayoutResult
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.drawText
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.xymusic.app.R
import com.xymusic.app.core.model.media.LyricsTiming
import kotlin.math.abs
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch

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
    val displayPosition = playbackPosition ?: rememberPlaybackPositionSnapshotState(uiState.player)
    var currentLyricIndex by remember(
        uiState.player.currentQueueItemId,
        uiState.lyrics,
        uiState.synchronizedLyrics,
    ) {
        mutableIntStateOf(-1)
    }
    LaunchedEffect(uiState.lyrics, uiState.synchronizedLyrics, displayPosition) {
        snapshotFlow {
            if (uiState.synchronizedLyrics) {
                playbackLyricIndex(uiState.lyrics, displayPosition.value.toLong())
            } else {
                -1
            }
        }
            .distinctUntilChanged()
            .collect { index ->
                currentLyricIndex = index
            }
    }
    var autoFollow by rememberSaveable(uiState.player.currentItem?.trackId) { mutableStateOf(true) }
    val lyricLineStyle = lyricLineStyle(compact)
    val lineTextStyle =
        LocalTextStyle.current.merge(
            TextStyle(
                fontSize = lyricLineStyle.fontSize,
                lineHeight = lyricLineStyle.lineHeight,
                fontWeight = FontWeight.Bold,
                letterSpacing = 0.sp,
            ),
        )
    val wordTimed = uiState.lyricsTiming == LyricsTiming.WORD
    val activeLinePosition = remember(uiState.player.currentQueueItemId) {
        Animatable(currentLyricIndex.toFloat())
    }
    val activeLinePositionState = remember(activeLinePosition) { activeLinePosition.asState() }
    var pendingLyricSeek by remember(
        uiState.player.currentQueueItemId,
        uiState.player.currentItem?.trackId,
    ) {
        mutableStateOf<LyricSeekRequest?>(null)
    }

    LaunchedEffect(isDragged) {
        if (isDragged) autoFollow = false
    }
    BoxWithConstraints(modifier = modifier.fillMaxSize()) {
        LaunchedEffect(
            uiState.synchronizedLyrics,
            uiState.player.currentItem?.trackId,
            uiState.player.currentQueueItemId,
            uiState.lyrics,
            centerActiveLine,
            maxHeight,
        ) {
            activeLinePosition.snapTo(currentLyricIndex.toFloat())
            var settledLinePosition = currentLyricIndex.toFloat()
            var transitionVelocity = 0f
            var previousLyricIndex: Int? = null
            var previousAutoFollow = autoFollow
            snapshotFlow {
                LyricPlaybackSnapshot(
                    lyricIndex = currentLyricIndex,
                    pendingSeek = pendingLyricSeek,
                    autoFollow = autoFollow,
                )
            }
                .distinctUntilChanged()
                .collectLatest { snapshot ->
                    val lyricIndex = snapshot.lyricIndex
                    val resumedAutoFollow = snapshot.autoFollow && !previousAutoFollow
                    previousAutoFollow = snapshot.autoFollow
                    if (lyricIndex < 0) {
                        previousLyricIndex = null
                        activeLinePosition.snapTo(-1f)
                        settledLinePosition = -1f
                        transitionVelocity = 0f
                        return@collectLatest
                    }
                    snapshot.pendingSeek?.let { request ->
                        val baselineIndex =
                            lyricSeekBaselineIndex(
                                sourceIndex = request.sourceIndex,
                                targetIndex = request.targetIndex,
                                currentIndex = lyricIndex,
                            ) ?: return@collectLatest
                        if (snapshot.autoFollow) {
                            listState.followLyricLine(
                                index = centeredLyricTargetIndex(baselineIndex, centerActiveLine),
                                centerActiveLine = centerActiveLine,
                                scrollMode = LyricFollowScrollMode.Snap,
                            )
                            awaitLyricLayoutStabilization(
                                listState = listState,
                                index = centeredLyricTargetIndex(baselineIndex, centerActiveLine),
                                centerActiveLine = centerActiveLine,
                            )
                        }
                        activeLinePosition.snapTo(baselineIndex.toFloat())
                        settledLinePosition = baselineIndex.toFloat()
                        transitionVelocity = 0f
                        previousLyricIndex = baselineIndex
                        pendingLyricSeek = null
                        return@collectLatest
                    }
                    if (lyricIndex == previousLyricIndex) {
                        if (resumedAutoFollow) {
                            listState.followLyricLine(
                                index = centeredLyricTargetIndex(lyricIndex, centerActiveLine),
                                centerActiveLine = centerActiveLine,
                                scrollMode = LyricFollowScrollMode.Snap,
                            )
                            activeLinePosition.snapTo(lyricIndex.toFloat())
                            settledLinePosition = lyricIndex.toFloat()
                            transitionVelocity = 0f
                        }
                        return@collectLatest
                    }
                    val previousIndex = previousLyricIndex
                    previousLyricIndex = lyricIndex
                    val targetIndex = centeredLyricTargetIndex(lyricIndex, centerActiveLine)
                    val scrollMode =
                        lyricFollowScrollMode(
                            previousLyricIndex = previousIndex,
                            lyricIndex = lyricIndex,
                            previousLyricTimeMs = previousIndex?.let { index ->
                                uiState.lyrics.getOrNull(index)?.timeMs
                            },
                            lyricTimeMs = uiState.lyrics.getOrNull(lyricIndex)?.timeMs,
                        )
                    when (scrollMode) {
                        LyricFollowScrollMode.Snap -> {
                            activeLinePosition.snapTo(lyricIndex.toFloat())
                            settledLinePosition = lyricIndex.toFloat()
                            transitionVelocity = 0f
                            if (snapshot.autoFollow) {
                                listState.followLyricLine(
                                    index = targetIndex,
                                    centerActiveLine = centerActiveLine,
                                    scrollMode = scrollMode,
                                )
                            }
                        }
                        LyricFollowScrollMode.Animate -> {
                            var transitionCompleted = false
                            try {
                                animateLyricLineTransition(
                                    activeLinePosition = activeLinePosition,
                                    listState = listState,
                                    startLinePosition = lyricTransitionStartPosition(settledLinePosition),
                                    lyricIndex = lyricIndex,
                                    targetIndex = targetIndex,
                                    centerActiveLine = centerActiveLine,
                                    autoFollow = snapshot.autoFollow,
                                    initialVelocity = transitionVelocity,
                                    preserveVelocity = previousIndex != null &&
                                        abs(settledLinePosition - previousIndex.toFloat()) > 0.01f,
                                    onFrame = { transitionVelocity = it },
                                )
                                transitionCompleted = true
                            } finally {
                                settledLinePosition = activeLinePosition.value
                                if (transitionCompleted) transitionVelocity = 0f
                            }
                            settledLinePosition = lyricIndex.toFloat()
                        }
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
                modifier = Modifier
                    .fillMaxSize()
                    .clipToBounds()
                    .testTag(PlayerTestTags.LyricsList),
                contentPadding =
                PaddingValues(
                    horizontal = if (compact) 8.dp else 16.dp,
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
                    val active = uiState.synchronizedLyrics && index == currentLyricIndex
                    val lineEmphasis = remember(activeLinePositionState, index) {
                        derivedStateOf { lyricLineEmphasis(activeLinePositionState.value, index) }
                    }
                    val interactionSource = remember { MutableInteractionSource() }
                    val isPressed by interactionSource.collectIsPressedAsState()
                    val pressScale by animateFloatAsState(
                        targetValue = if (isPressed) 0.965f else 1f,
                        animationSpec = tween(
                            durationMillis = LyricAnimationConstants.PRESS_ANIMATION_DURATION_MILLIS,
                            easing = FastOutSlowInEasing,
                        ),
                        label = "lyricLinePressScale",
                    )
                    val lineModifier =
                        Modifier
                            .fillMaxWidth()
                            .testTag(PlayerTestTags.lyricLine(index))
                            .graphicsLayer {
                                transformOrigin = TransformOrigin(0f, 0.5f)
                                scaleX = pressScale
                                scaleY = pressScale
                            }
                            .clickable(
                                interactionSource = interactionSource,
                                indication = null,
                                enabled = uiState.synchronizedLyrics && line.timeMs != null,
                                role = Role.Button,
                                onClick = {
                                    line.timeMs?.let { timestamp ->
                                        pendingLyricSeek = LyricSeekRequest(
                                            sourceIndex = currentLyricIndex.takeIf { it >= 0 }
                                                ?: index,
                                            targetIndex = index,
                                        )
                                        autoFollow = true
                                        onSeek(timestamp)
                                    }
                                },
                            ).semantics {
                                if (uiState.synchronizedLyrics) selected = active
                            }
                    if (wordTimed && line.timeMs != null && line.words.isNotEmpty()) {
                        WordByWordLyricText(
                            text = line.text,
                            words = line.words,
                            playbackPosition = displayPosition,
                            isActive = active,
                            modifier = lineModifier,
                            baseColor = PlayerMutedContent,
                            highlightColor = PlayerPrimaryContent,
                            style = lineTextStyle,
                        )
                    } else if (uiState.synchronizedLyrics) {
                        AnimatedLyricLineText(
                            text = line.text,
                            modifier = lineModifier,
                            lineEmphasis = lineEmphasis,
                            baseColor = PlayerMutedContent,
                            highlightColor = PlayerPrimaryContent,
                            style = lineTextStyle,
                        )
                    } else {
                        Text(
                            text = line.text,
                            modifier = lineModifier,
                            color = PlayerPrimaryContent.copy(alpha = 0.88f),
                            style = lineTextStyle,
                            softWrap = true,
                            overflow = TextOverflow.Clip,
                            maxLines = Int.MAX_VALUE,
                        )
                    }
                }
            }
        }
        if (!autoFollow && uiState.synchronizedLyrics) {
            FilledTonalButton(
                onClick = {
                    autoFollow = true
                    pendingLyricSeek = null
                },
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

internal fun lyricFollowScrollMode(
    previousLyricIndex: Int?,
    lyricIndex: Int,
    previousLyricTimeMs: Long? = null,
    lyricTimeMs: Long? = null,
): LyricFollowScrollMode {
    val indexGap =
        previousLyricIndex?.let { previous ->
            abs(lyricIndex - previous)
        } ?: return LyricFollowScrollMode.Snap
    val denseNaturalAdvance =
        previousLyricTimeMs != null &&
            lyricTimeMs != null &&
            lyricTimeMs >= previousLyricTimeMs &&
            lyricTimeMs - previousLyricTimeMs <= LyricAnimationConstants.DENSE_LYRIC_TIME_GAP_MILLIS
    return if (indexGap > 1 && !denseNaturalAdvance) {
        LyricFollowScrollMode.Snap
    } else {
        LyricFollowScrollMode.Animate
    }
}

internal fun lyricTransitionStartPosition(activeLinePosition: Float): Float = activeLinePosition

internal fun lyricTransitionDurationMillis(lineDistance: Float, scrollDistance: Float): Int {
    val visualDistancePx = maxOf(
        abs(lineDistance) * LyricAnimationConstants.TRANSITION_LINE_DISTANCE_PX,
        abs(scrollDistance),
    )
    return (visualDistancePx / LyricAnimationConstants.TRANSITION_PIXELS_PER_SECOND * 1_000f)
        .toInt()
        .coerceIn(
            LyricAnimationConstants.TRANSITION_MIN_DURATION_MILLIS,
            LyricAnimationConstants.TRANSITION_MAX_DURATION_MILLIS,
        )
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
        LyricFollowScrollMode.Animate -> error("Animated lyric transitions must use the shared transition clock")
    }
}

private suspend fun LazyListState.snapScrollToCenteredItem(index: Int) {
    if (centeredItemDelta(index)?.let { abs(it) <= 0.5f } == true) return
    scrollToItem(index)
    withFrameNanos {}
    centeredItemDelta(index)?.let { scrollBy(it) }
    withFrameNanos {}
    val residual = centeredItemDelta(index) ?: return
    if (abs(residual) > 0.5f) scrollBy(residual)
}

internal fun lyricLayoutDeltaHasSettled(previousDelta: Float?, currentDelta: Float?): Boolean = previousDelta != null &&
    currentDelta != null &&
    abs(currentDelta - previousDelta) <= LyricAnimationConstants.LAYOUT_STABILITY_EPSILON_PX

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

private suspend fun awaitLyricLayoutStabilization(listState: LazyListState, index: Int, centerActiveLine: Boolean) {
    var previousDelta: Float? = null
    var stableFrameCount = 0
    repeat(LyricAnimationConstants.BASELINE_MAX_FRAME_COUNT) {
        withFrameNanos {}
        val currentDelta =
            if (centerActiveLine) {
                listState.centeredItemDelta(index)
            } else {
                listState.alignedItemDelta(index)
            }
        if (lyricLayoutDeltaHasSettled(previousDelta, currentDelta)) {
            stableFrameCount += 1
            if (stableFrameCount >= LyricAnimationConstants.REQUIRED_STABLE_FRAMES) return
        } else {
            stableFrameCount = 0
        }
        previousDelta = currentDelta
    }
}

private suspend fun animateLyricLineTransition(
    activeLinePosition: Animatable<Float, AnimationVector1D>,
    listState: LazyListState,
    startLinePosition: Float,
    lyricIndex: Int,
    targetIndex: Int,
    centerActiveLine: Boolean,
    autoFollow: Boolean,
    initialVelocity: Float,
    preserveVelocity: Boolean,
    onFrame: (Float) -> Unit,
) {
    if (autoFollow) {
        awaitLyricItemLayout(listState, targetIndex)
    }
    val initialScrollDelta =
        if (autoFollow) {
            if (centerActiveLine) {
                listState.centeredItemDelta(targetIndex)
            } else {
                listState.alignedItemDelta(targetIndex)
            }
        } else {
            null
        }
    val lineDistance = lyricIndex - startLinePosition
    val scrollDistance = initialScrollDelta ?: 0f
    val transitionDurationMillis = lyricTransitionDurationMillis(
        lineDistance = lineDistance,
        scrollDistance = scrollDistance,
    )
    val animationSpec: FiniteAnimationSpec<Float> = if (preserveVelocity) {
        spring()
    } else {
        tween(
            durationMillis = transitionDurationMillis,
            easing = FastOutSlowInEasing,
        )
    }

    if (autoFollow && initialScrollDelta == null) {
        coroutineScope {
            launch {
                activeLinePosition.animateTo(
                    targetValue = lyricIndex.toFloat(),
                    animationSpec = animationSpec,
                    initialVelocity = initialVelocity,
                ) {
                    onFrame(velocity)
                }
                onFrame(0f)
            }
            launch {
                listState.animateLyricLineIntoLayout(
                    index = targetIndex,
                    centerActiveLine = centerActiveLine,
                    correctionDurationMillis = transitionDurationMillis,
                )
            }
        }
        return
    }

    var appliedScroll = 0f
    activeLinePosition.animateTo(
        targetValue = lyricIndex.toFloat(),
        animationSpec = animationSpec,
        initialVelocity = initialVelocity,
    ) {
        onFrame(velocity)
        val progress =
            if (abs(lineDistance) <= 0.01f) {
                1f
            } else {
                ((value - startLinePosition) / lineDistance).coerceIn(0f, 1f)
            }
        val nextScroll = scrollDistance * progress
        val scrollDelta = nextScroll - appliedScroll
        if (abs(scrollDelta) > 0.01f) {
            appliedScroll += listState.dispatchRawDelta(scrollDelta)
        }
    }
    if (abs(scrollDistance - appliedScroll) > 0.01f) {
        listState.scrollBy(scrollDistance - appliedScroll)
    }
    if (autoFollow && centerActiveLine) {
        listState.centeredItemDelta(targetIndex)?.let { residual ->
            if (abs(residual) > 0.5f) listState.scrollBy(residual)
        }
    }
}

private suspend fun LazyListState.animateLyricLineIntoLayout(
    index: Int,
    centerActiveLine: Boolean,
    correctionDurationMillis: Int,
) {
    animateScrollToItem(index)
    awaitLyricItemLayout(listState = this, index = index)
    val delta = if (centerActiveLine) centeredItemDelta(index) else alignedItemDelta(index)
    if (delta != null && abs(delta) > 0.5f) {
        animateScrollBy(
            value = delta,
            animationSpec = tween(
                durationMillis = correctionDurationMillis,
                easing = FastOutSlowInEasing,
            ),
        )
    }
    repeat(LyricAnimationConstants.LAYOUT_CORRECTION_PASS_COUNT) {
        withFrameNanos {}
        val residual = if (centerActiveLine) centeredItemDelta(index) else alignedItemDelta(index)
        if (residual != null && abs(residual) > 0.5f) scrollBy(residual)
    }
}

private suspend fun awaitLyricItemLayout(listState: LazyListState, index: Int) {
    repeat(LyricAnimationConstants.ITEM_LAYOUT_MAX_FRAME_COUNT) {
        if (listState.layoutInfo.visibleItemsInfo.any { it.index == index }) return
        withFrameNanos {}
    }
}

private fun lyricLineStyle(compact: Boolean): LyricLineStyle = if (compact) {
    LyricLineStyle(
        fontSize = 30.sp,
        lineHeight = 42.sp,
    )
} else {
    LyricLineStyle(
        fontSize = 36.sp,
        lineHeight = 50.sp,
    )
}

private fun lyricLineEmphasis(animatedLinePosition: Float, lineIndex: Int): Float = if (animatedLinePosition < 0f) {
    0f
} else {
    (1f - abs(animatedLinePosition - lineIndex)).coerceIn(0f, 1f)
}

internal fun lyricWordHighlightEmphasis(isActive: Boolean, lineEmphasis: Float): Float =
    if (isActive && lineEmphasis >= LYRIC_LINE_SETTLED_EMPHASIS) 1f else 0f

private const val LYRIC_LINE_SETTLED_EMPHASIS = 0.999f

private fun centeredLyricTargetIndex(index: Int, centerActiveLine: Boolean): Int =
    if (centerActiveLine) index else (index - 2).coerceAtLeast(0)

private data class LyricSeekRequest(val sourceIndex: Int, val targetIndex: Int)

private data class LyricPlaybackSnapshot(
    val lyricIndex: Int,
    val pendingSeek: LyricSeekRequest?,
    val autoFollow: Boolean,
)

internal fun lyricSeekBaselineIndex(sourceIndex: Int, targetIndex: Int, currentIndex: Int): Int? = when {
    currentIndex == targetIndex -> targetIndex
    targetIndex > sourceIndex && currentIndex > targetIndex -> targetIndex
    targetIndex < sourceIndex && currentIndex < targetIndex -> targetIndex
    else -> null
}

@Composable
private fun AnimatedLyricLineText(
    text: String,
    modifier: Modifier,
    lineEmphasis: State<Float>,
    baseColor: Color,
    highlightColor: Color,
    style: TextStyle,
) {
    val drawCache = remember(text) { LyricLineDrawCache() }
    Text(
        text = text,
        modifier = modifier.drawWithContent {
            drawContent()
            drawCache.drawHighlight(
                drawScope = this,
                highlightColor = highlightColor,
                alpha = lineEmphasis.value,
            )
        },
        style = style,
        color = baseColor,
        softWrap = true,
        overflow = TextOverflow.Clip,
        maxLines = Int.MAX_VALUE,
        onTextLayout = drawCache::updateLayout,
    )
}

private class LyricLineDrawCache {
    private var layoutResult: TextLayoutResult? = null

    fun updateLayout(layoutResult: TextLayoutResult) {
        this.layoutResult = layoutResult
    }

    fun drawHighlight(drawScope: ContentDrawScope, highlightColor: Color, alpha: Float) {
        if (alpha <= 0f) return
        layoutResult?.let { layout ->
            with(drawScope) {
                drawText(layout, color = highlightColor, alpha = alpha.coerceIn(0f, 1f))
            }
        }
    }
}

private data class LyricLineStyle(val fontSize: TextUnit, val lineHeight: TextUnit)

private object LyricAnimationConstants {
    const val TRANSITION_MIN_DURATION_MILLIS = 300
    const val TRANSITION_MAX_DURATION_MILLIS = 520
    const val TRANSITION_LINE_DISTANCE_PX = 56f
    const val TRANSITION_PIXELS_PER_SECOND = 185f
    const val PRESS_ANIMATION_DURATION_MILLIS = 180
    const val DENSE_LYRIC_TIME_GAP_MILLIS = 450L
    const val BASELINE_MAX_FRAME_COUNT = 8
    const val REQUIRED_STABLE_FRAMES = 2
    const val ITEM_LAYOUT_MAX_FRAME_COUNT = 6
    const val LAYOUT_CORRECTION_PASS_COUNT = 3
    const val LAYOUT_STABILITY_EPSILON_PX = 0.5f
}
