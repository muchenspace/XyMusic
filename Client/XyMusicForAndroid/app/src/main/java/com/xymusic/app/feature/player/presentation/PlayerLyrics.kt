package com.xymusic.app.feature.player.presentation

import android.os.SystemClock
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.AnimationVector1D
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.FiniteAnimationSpec
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.spring
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
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.runtime.snapshots.Snapshot
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
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.collect
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
    val displayPosition = playbackPosition ?: rememberPlaybackPositionSnapshotState(uiState.player)
    var currentLyricIndex by remember(
        uiState.player.currentQueueItemId,
        uiState.player.currentItem?.trackId,
        uiState.lyrics,
        uiState.synchronizedLyrics,
    ) {
        mutableIntStateOf(
            if (uiState.synchronizedLyrics) {
                playbackLyricIndex(
                    uiState.lyrics,
                    Snapshot.withoutReadObservation { displayPosition.value }.toLong(),
                )
            } else {
                -1
            },
        )
    }
    LaunchedEffect(
        uiState.player.currentQueueItemId,
        uiState.player.currentItem?.trackId,
        uiState.lyrics,
        uiState.synchronizedLyrics,
        displayPosition,
    ) {
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
    var autoFollow by rememberSaveable(
        uiState.player.currentItem?.trackId,
        uiState.player.currentQueueItemId,
    ) { mutableStateOf(true) }
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
    val emphasisPhase = remember(
        uiState.player.currentQueueItemId,
        uiState.lyrics,
        uiState.synchronizedLyrics,
    ) {
        Animatable(0f)
    }
    val emphasisPhaseState = remember(emphasisPhase) { emphasisPhase.asState() }
    var pendingLyricSeek by remember(
        uiState.player.currentQueueItemId,
        uiState.player.currentItem?.trackId,
        uiState.lyrics,
    ) {
        mutableStateOf<LyricSeekRequest?>(null)
    }
    var emphasisTransition by remember(
        uiState.player.currentQueueItemId,
        uiState.lyrics,
        uiState.synchronizedLyrics,
    ) {
        mutableStateOf(LyricEmphasisTransition.settled(currentLyricIndex))
    }

    LaunchedEffect(isDragged) {
        if (isDragged) autoFollow = false
    }
    LaunchedEffect(pendingLyricSeek) {
        val request = pendingLyricSeek ?: return@LaunchedEffect
        delay(LyricAnimationConstants.SEEK_ACK_TIMEOUT_MILLIS)
        if (pendingLyricSeek == request) pendingLyricSeek = null
    }
    BoxWithConstraints(modifier = modifier.fillMaxSize()) {
        LaunchedEffect(
            uiState.synchronizedLyrics,
            uiState.player.currentItem?.trackId,
            uiState.player.currentQueueItemId,
            uiState.lyrics,
            centerActiveLine,
        ) {
            emphasisTransition = LyricEmphasisTransition.settled(currentLyricIndex)
            emphasisPhase.snapTo(0f)
            var activeLinePosition = currentLyricIndex.toFloat()
            var settledLinePosition = currentLyricIndex.toFloat()
            var transitionPhaseVelocity = 0f
            var previousLyricIndexCollected = currentLyricIndex.takeIf { it >= 0 }
            var previousAutoFollow = autoFollow

            if (uiState.lyrics.isNotEmpty()) {
                if (currentLyricIndex >= 0 && autoFollow) {
                    listState.followLyricLine(
                        index = centeredLyricTargetIndex(currentLyricIndex, centerActiveLine),
                        centerActiveLine = centerActiveLine,
                        scrollMode = LyricFollowScrollMode.Snap,
                    )
                } else if (currentLyricIndex < 0) {
                    listState.scrollToItem(0)
                }
            }

            suspend fun settleLineEmphasis(lineIndex: Int) {
                val startLinePosition = activeLinePosition
                if (!startLinePosition.isFinite() || !emphasisPhase.value.isFinite()) {
                    activeLinePosition = lineIndex.toFloat()
                    settledLinePosition = activeLinePosition
                    emphasisTransition = LyricEmphasisTransition.settled(lineIndex)
                    emphasisPhase.snapTo(0f)
                    transitionPhaseVelocity = 0f
                    return
                }
                if (!lyricTransitionNeedsSettling(
                        animatedLinePosition = startLinePosition,
                        emphasisPhase = emphasisPhase.value,
                        lineIndex = lineIndex,
                        transition = emphasisTransition,
                    )
                ) {
                    activeLinePosition = lineIndex.toFloat()
                    emphasisTransition = LyricEmphasisTransition.settled(lineIndex)
                    emphasisPhase.snapTo(0f)
                    settledLinePosition = lineIndex.toFloat()
                    transitionPhaseVelocity = 0f
                    return
                }
                emphasisTransition =
                    emphasisTransition.retarget(
                        emphasisPhase = emphasisPhase.value,
                        targetLineIndex = lineIndex,
                    )
                val startEmphasisPhase = emphasisTransition.startPhase
                val targetEmphasisPhase = emphasisTransition.endPhase
                var transitionCompleted = false
                try {
                    animateLyricLineTransition(
                        emphasisPhase = emphasisPhase,
                        startEmphasisPhase = startEmphasisPhase,
                        targetEmphasisPhase = targetEmphasisPhase,
                        listState = listState,
                        startLinePosition = startLinePosition,
                        lyricIndex = lineIndex,
                        targetIndex = centeredLyricTargetIndex(lineIndex, centerActiveLine),
                        centerActiveLine = centerActiveLine,
                        autoFollow = false,
                        initialPhaseVelocity = transitionPhaseVelocity,
                        preserveVelocity = abs(startLinePosition - lineIndex.toFloat()) > LYRIC_POSITION_EPSILON,
                        onFrame = { linePosition, phaseVelocity ->
                            activeLinePosition = linePosition
                            transitionPhaseVelocity = phaseVelocity
                        },
                    )
                    transitionCompleted = true
                } finally {
                    settledLinePosition = activeLinePosition
                    if (transitionCompleted) {
                        emphasisTransition = LyricEmphasisTransition.settled(lineIndex)
                        emphasisPhase.snapTo(0f)
                        settledLinePosition = lineIndex.toFloat()
                        activeLinePosition = settledLinePosition
                        transitionPhaseVelocity = 0f
                    }
                }
            }

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
                        previousLyricIndexCollected = null
                        emphasisTransition = LyricEmphasisTransition.Empty
                        emphasisPhase.snapTo(0f)
                        activeLinePosition = -1f
                        settledLinePosition = -1f
                        transitionPhaseVelocity = 0f
                        return@collectLatest
                    }
                    snapshot.pendingSeek?.let { request ->
                        val baselineIndex =
                            lyricSeekBaselineIndex(
                                sourceIndex = request.sourceIndex,
                                targetIndex = request.targetIndex,
                                currentIndex = lyricIndex,
                            )
                        if (baselineIndex != null) {
                            activeLinePosition = baselineIndex.toFloat()
                            settledLinePosition = activeLinePosition
                            emphasisTransition = LyricEmphasisTransition.settled(baselineIndex)
                            emphasisPhase.snapTo(0f)
                            transitionPhaseVelocity = 0f
                            previousLyricIndexCollected = baselineIndex
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
                            pendingLyricSeek = null
                            return@collectLatest
                        }
                        if (!request.isExpired(SystemClock.elapsedRealtime())) return@collectLatest
                        // A failed or clamped seek must not suppress every later auto-follow
                        // transition.  Continue from the position actually acknowledged by the
                        // player once the pending request has aged out.
                        pendingLyricSeek = null
                    }
                    if (lyricIndex == previousLyricIndexCollected) {
                        if (resumedAutoFollow) {
                            listState.followLyricLine(
                                index = centeredLyricTargetIndex(lyricIndex, centerActiveLine),
                                centerActiveLine = centerActiveLine,
                                scrollMode = LyricFollowScrollMode.Snap,
                            )
                        }
                        if (
                            lyricTransitionNeedsSettling(
                                animatedLinePosition = activeLinePosition,
                                emphasisPhase = emphasisPhase.value,
                                lineIndex = lyricIndex,
                                transition = emphasisTransition,
                            )
                        ) {
                            settleLineEmphasis(lyricIndex)
                        }
                        return@collectLatest
                    }
                    val previousIndex = previousLyricIndexCollected
                    previousLyricIndexCollected = lyricIndex
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
                            activeLinePosition = lyricIndex.toFloat()
                            emphasisTransition = LyricEmphasisTransition.settled(lyricIndex)
                            emphasisPhase.snapTo(0f)
                            settledLinePosition = lyricIndex.toFloat()
                            transitionPhaseVelocity = 0f
                            if (snapshot.autoFollow) {
                                listState.followLyricLine(
                                    index = targetIndex,
                                    centerActiveLine = centerActiveLine,
                                    scrollMode = scrollMode,
                                )
                            }
                        }
                        LyricFollowScrollMode.Animate -> {
                            val transitionStartPosition = lyricTransitionStartPosition(settledLinePosition)
                            emphasisTransition =
                                emphasisTransition.retarget(
                                    emphasisPhase = emphasisPhase.value,
                                    targetLineIndex = lyricIndex,
                                )
                            val startEmphasisPhase = emphasisTransition.startPhase
                            val targetEmphasisPhase = emphasisTransition.endPhase
                            var transitionCompleted = false
                            try {
                                animateLyricLineTransition(
                                    emphasisPhase = emphasisPhase,
                                    startEmphasisPhase = startEmphasisPhase,
                                    targetEmphasisPhase = targetEmphasisPhase,
                                    listState = listState,
                                    startLinePosition = transitionStartPosition,
                                    lyricIndex = lyricIndex,
                                    targetIndex = targetIndex,
                                    centerActiveLine = centerActiveLine,
                                    autoFollow = snapshot.autoFollow,
                                    initialPhaseVelocity = transitionPhaseVelocity,
                                    preserveVelocity = previousIndex != null &&
                                        abs(settledLinePosition - previousIndex.toFloat()) > 0.01f,
                                    onFrame = { linePosition, phaseVelocity ->
                                        activeLinePosition = linePosition
                                        transitionPhaseVelocity = phaseVelocity
                                    },
                                )
                                transitionCompleted = true
                            } finally {
                                settledLinePosition = activeLinePosition
                                if (transitionCompleted) {
                                    transitionPhaseVelocity = 0f
                                    emphasisTransition = LyricEmphasisTransition.settled(lyricIndex)
                                    emphasisPhase.snapTo(0f)
                                    activeLinePosition = lyricIndex.toFloat()
                                }
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
                    val lineEmphasis = remember(emphasisPhaseState, index) {
                        derivedStateOf {
                            lyricLineTransitionEmphasis(
                                emphasisPhase = emphasisPhaseState.value,
                                lineIndex = index,
                                transition = emphasisTransition,
                            )
                        }
                    }
                    val interactionSource = remember { MutableInteractionSource() }
                    val animatedScale = remember(lineEmphasis) {
                        derivedStateOf {
                            // Keep the focus cue below the threshold at which repeated scale
                            // changes become visually tiring, especially on 90/120 Hz panels.
                            // The actual line movement remains on the shared scroll clock.
                            1f + 0.012f * lineEmphasis.value
                        }
                    }
                    val animatedAlpha = remember(uiState.synchronizedLyrics) {
                        derivedStateOf {
                            if (uiState.synchronizedLyrics) 1f else 0.88f
                        }
                    }
                    val lineModifier =
                        Modifier
                            .fillMaxWidth()
                            .testTag(PlayerTestTags.lyricLine(index))
                            .graphicsLayer {
                                transformOrigin = TransformOrigin(0f, 0.5f)
                                scaleX = animatedScale.value
                                scaleY = animatedScale.value
                                alpha = animatedAlpha.value
                            }
                            .clickable(
                                interactionSource = interactionSource,
                                indication = null,
                                enabled = uiState.synchronizedLyrics && line.timeMs != null,
                                role = Role.Button,
                                onClick = {
                                    line.timeMs?.let { timestamp ->
                                        val targetIndex = canonicalLyricTargetIndex(uiState.lyrics, index)
                                        pendingLyricSeek = LyricSeekRequest(
                                            sourceIndex = currentLyricIndex.takeIf { it >= 0 }
                                                ?: index,
                                            targetIndex = targetIndex,
                                            requestedAtElapsedRealtimeMs = SystemClock.elapsedRealtime(),
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
                            lineIndex = index,
                            currentLyricIndex = currentLyricIndex,
                            lineEmphasis = lineEmphasis,
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
    // A decreasing lyric index is a seek/repeat discontinuity, not natural playback.  Reversing
    // the whole list animation is distracting and can briefly highlight already-finished words.
    if (previousLyricIndex != null && lyricIndex < previousLyricIndex) {
        return LyricFollowScrollMode.Snap
    }
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
    previousDelta.isFinite() &&
    currentDelta.isFinite() &&
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

/**
 * Estimates an off-screen item's alignment from the nearest laid-out item.  LazyColumn cannot
 * report an exact target delta until that item is visible; using the measured neighbour keeps the
 * first movement on the same frame clock as the line emphasis instead of starting a second,
 * unrelated animateScrollToItem clock.
 */
private fun LazyListState.estimatedItemDelta(index: Int, centerActiveLine: Boolean): Float? {
    val info = layoutInfo
    val visibleItems = info.visibleItemsInfo
    if (visibleItems.isEmpty()) return null
    val exact = if (centerActiveLine) centeredItemDelta(index) else alignedItemDelta(index)
    if (exact != null) return exact

    val nearest = visibleItems.minByOrNull { item -> abs(item.index - index) } ?: return null
    val averageItemSize = visibleItems.map { item -> item.size }.average().toFloat()
    val averageStride =
        if (visibleItems.size > 1) {
            val first = visibleItems.first()
            val last = visibleItems.last()
            val indexDistance = abs(last.index - first.index).coerceAtLeast(1)
            abs(last.offset - first.offset).toFloat() / indexDistance
        } else {
            nearest.size.toFloat()
        }.coerceAtLeast(1f)
    val indexDistance = index - nearest.index
    val estimatedStart =
        if (indexDistance > 0) {
            nearest.offset + nearest.size + (indexDistance - 1) * averageStride
        } else {
            nearest.offset + indexDistance * averageStride
        }
    val targetAnchor =
        if (centerActiveLine) {
            val viewportCenter = (info.viewportStartOffset + info.viewportEndOffset) / 2f
            estimatedStart + averageItemSize / 2f - viewportCenter
        } else {
            estimatedStart - info.viewportStartOffset
        }
    return targetAnchor
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
    emphasisPhase: Animatable<Float, AnimationVector1D>,
    startEmphasisPhase: Float,
    targetEmphasisPhase: Float,
    listState: LazyListState,
    startLinePosition: Float,
    lyricIndex: Int,
    targetIndex: Int,
    centerActiveLine: Boolean,
    autoFollow: Boolean,
    initialPhaseVelocity: Float,
    preserveVelocity: Boolean,
    onFrame: (linePosition: Float, phaseVelocity: Float) -> Unit,
) {
    val initialScrollDelta =
        if (autoFollow) {
            listState.estimatedItemDelta(targetIndex, centerActiveLine)
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
        spring(
            dampingRatio = Spring.DampingRatioNoBouncy,
            stiffness = Spring.StiffnessLow,
        )
    } else {
        tween(
            durationMillis = transitionDurationMillis,
            easing = FastOutSlowInEasing,
        )
    }

    val phaseDistance = targetEmphasisPhase - startEmphasisPhase
    val animationInitialVelocity = initialPhaseVelocity
        .takeIf { preserveVelocity && it.isFinite() }
        ?: 0f
    var appliedScroll = 0f
    emphasisPhase.animateTo(
        targetValue = targetEmphasisPhase,
        animationSpec = animationSpec,
        initialVelocity = animationInitialVelocity,
    ) {
        val progress =
            if (abs(phaseDistance) <= LYRIC_POSITION_EPSILON) {
                1f
            } else {
                ((value - startEmphasisPhase) / phaseDistance).coerceIn(0f, 1f)
            }
        val linePosition = startLinePosition + lineDistance * progress
        onFrame(linePosition, velocity)
        val nextScroll = scrollDistance * progress
        val scrollDelta = nextScroll - appliedScroll
        if (abs(scrollDelta) > 0.01f) {
            appliedScroll += listState.dispatchRawDelta(scrollDelta)
        }
    }
    onFrame(lyricIndex.toFloat(), 0f)
    if (autoFollow) {
        listState.correctLyricLineAlignment(
            index = targetIndex,
            centerActiveLine = centerActiveLine,
            correctionDurationMillis = transitionDurationMillis,
        )
    }
}

private suspend fun LazyListState.correctLyricLineAlignment(
    index: Int,
    centerActiveLine: Boolean,
    correctionDurationMillis: Int,
) {
    val correctionSpec =
        tween<Float>(
            durationMillis =
            correctionDurationMillis.coerceIn(
                LyricAnimationConstants.CORRECTION_MIN_DURATION_MILLIS,
                LyricAnimationConstants.CORRECTION_MAX_DURATION_MILLIS,
            ),
            easing = FastOutSlowInEasing,
        )
    repeat(LyricAnimationConstants.LAYOUT_CORRECTION_MAX_PASS_COUNT) {
        val measuredDelta = if (centerActiveLine) centeredItemDelta(index) else alignedItemDelta(index)
        val delta = measuredDelta ?: estimatedItemDelta(index, centerActiveLine) ?: return
        if (abs(delta) <= LyricAnimationConstants.LAYOUT_STABILITY_EPSILON_PX) {
            if (measuredDelta != null) return
            withFrameNanos {}
        } else {
            animateScrollBy(value = delta, animationSpec = correctionSpec)
            withFrameNanos {}
        }
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

/** Returns only the target and preserved interruption weights; skipped lines remain dark. */
internal fun lyricLineTransitionEmphasis(
    emphasisPhase: Float,
    lineIndex: Int,
    transition: LyricEmphasisTransition,
): Float = transition.emphasisAt(emphasisPhase, lineIndex)

/**
 * Captures the visible line weights at the start of a transition. Retargeting samples those
 * weights before changing the destination, so an interrupted dense transition cannot make its
 * previous target disappear for one frame or light a skipped line.
 */
internal data class LyricEmphasisTransition(
    val startPhase: Float,
    val endPhase: Float,
    val targetLineIndex: Int,
    val startEmphasis: Map<Int, Float>,
) {
    fun emphasisAt(emphasisPhase: Float, lineIndex: Int): Float {
        if (!emphasisPhase.isFinite() || lineIndex < 0 || targetLineIndex < 0) return 0f
        val progress = progressAt(emphasisPhase)
        val initial = startEmphasis[lineIndex] ?: 0f
        val target = if (lineIndex == targetLineIndex) 1f else 0f
        return (initial + (target - initial) * progress).coerceIn(0f, 1f)
    }

    fun retarget(emphasisPhase: Float, targetLineIndex: Int): LyricEmphasisTransition {
        if (!emphasisPhase.isFinite() || targetLineIndex < 0) return Empty
        val sampledEmphasis = buildMap {
            (startEmphasis.keys + this@LyricEmphasisTransition.targetLineIndex).forEach { lineIndex ->
                val emphasis = emphasisAt(emphasisPhase, lineIndex)
                if (emphasis > MIN_RETAINED_EMPHASIS) put(lineIndex, emphasis)
            }
        }
        return LyricEmphasisTransition(
            startPhase = emphasisPhase,
            endPhase = emphasisPhase + 1f,
            targetLineIndex = targetLineIndex,
            startEmphasis = sampledEmphasis,
        )
    }

    fun isSettledAt(emphasisPhase: Float, lineIndex: Int): Boolean =
        lineIndex == targetLineIndex && progressAt(emphasisPhase) >= 1f - LYRIC_POSITION_EPSILON

    private fun progressAt(emphasisPhase: Float): Float = when {
        endPhase <= startPhase -> 1f
        else -> ((emphasisPhase - startPhase) / (endPhase - startPhase)).coerceIn(0f, 1f)
    }

    companion object {
        val Empty = LyricEmphasisTransition(0f, 0f, -1, emptyMap())

        fun settled(lineIndex: Int): LyricEmphasisTransition = if (lineIndex >= 0) {
            LyricEmphasisTransition(
                startPhase = 0f,
                endPhase = 0f,
                targetLineIndex = lineIndex,
                startEmphasis = mapOf(lineIndex to 1f),
            )
        } else {
            Empty
        }

        // Below half of one 8-bit alpha step after easing, a discarded residual cannot produce a
        // visible flash. The threshold also bounds the sparse map because its weights sum to one.
        private const val MIN_RETAINED_EMPHASIS = 1f / 1_024f
    }
}

internal fun lyricTransitionNeedsSettling(
    animatedLinePosition: Float,
    emphasisPhase: Float,
    lineIndex: Int,
    transition: LyricEmphasisTransition,
): Boolean = when {
    lineIndex < 0 -> false
    !animatedLinePosition.isFinite() -> true
    !emphasisPhase.isFinite() -> true
    abs(animatedLinePosition - lineIndex.toFloat()) > LYRIC_POSITION_EPSILON -> true
    else -> !transition.isSettledAt(emphasisPhase, lineIndex)
}

private fun centeredLyricTargetIndex(index: Int, centerActiveLine: Boolean): Int =
    if (centerActiveLine) index else (index - 2).coerceAtLeast(0)

private data class LyricSeekRequest(
    val sourceIndex: Int,
    val targetIndex: Int,
    val requestedAtElapsedRealtimeMs: Long,
) {
    fun isExpired(nowElapsedRealtimeMs: Long): Boolean =
        nowElapsedRealtimeMs - requestedAtElapsedRealtimeMs >= LyricAnimationConstants.SEEK_ACK_TIMEOUT_MILLIS
}

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

/** Playback resolves equal timestamps to the last line, so clicks use that same canonical target. */
internal fun canonicalLyricTargetIndex(lines: List<PlayerLyricLineUi>, requestedIndex: Int): Int {
    val requestedTime = lines.getOrNull(requestedIndex)?.timeMs ?: return requestedIndex
    return lines.indexOfLast { line -> line.timeMs == requestedTime }.takeIf { it >= 0 } ?: requestedIndex
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
    const val CORRECTION_MIN_DURATION_MILLIS = 90
    const val CORRECTION_MAX_DURATION_MILLIS = 180
    const val DENSE_LYRIC_TIME_GAP_MILLIS = 450L
    const val BASELINE_MAX_FRAME_COUNT = 8
    const val REQUIRED_STABLE_FRAMES = 2
    const val LAYOUT_CORRECTION_MAX_PASS_COUNT = 4
    const val LAYOUT_STABILITY_EPSILON_PX = 0.5f
    const val SEEK_ACK_TIMEOUT_MILLIS = 1_500L
}

private const val LYRIC_POSITION_EPSILON = 0.01f
