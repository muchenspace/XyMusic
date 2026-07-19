package com.xymusic.app.feature.player.presentation

import androidx.compose.foundation.layout.Box
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.State
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.ContentDrawScope
import androidx.compose.ui.graphics.drawscope.clipPath
import androidx.compose.ui.graphics.drawscope.clipRect
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.text.TextLayoutResult
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.style.ResolvedTextDirection
import kotlin.math.floor

@Composable
internal fun WordByWordLyricText(
    text: String,
    highlightEndOffsets: List<Int>,
    playbackPosition: State<Float>,
    lineStartTimeMs: Long,
    lineEndTimeMs: Long,
    modifier: Modifier = Modifier,
    baseColor: Color,
    highlightColor: Color,
    style: TextStyle,
) {
    val normalizedHighlightEndOffsets =
        remember(text, highlightEndOffsets) {
            highlightEndOffsets
                .asSequence()
                .filter { offset -> offset in 1..text.length }
                .distinct()
                .sorted()
                .toList()
        }
    val drawCache =
        remember(text, normalizedHighlightEndOffsets) {
            WordByWordLyricDrawCache(normalizedHighlightEndOffsets)
        }

    Box(modifier = modifier) {
        Text(
            text = text,
            color = baseColor,
            style = style,
        )
        Text(
            text = text,
            modifier =
            Modifier
                .clearAndSetSemantics {}
                .drawWithContent {
                    val progress =
                        calculateWordByWordHighlightProgress(
                            playbackPositionMs = playbackPosition.value,
                            lineStartTimeMs = lineStartTimeMs,
                            lineEndTimeMs = lineEndTimeMs,
                            graphemeCount = normalizedHighlightEndOffsets.size,
                        )
                    drawCache.drawHighlight(this, progress)
                },
            color = highlightColor,
            style = style,
            onTextLayout = drawCache::updateLayout,
        )
    }
}

internal data class WordByWordHighlightProgress(val completedCount: Int, val currentFraction: Float)

internal fun calculateWordByWordHighlightProgress(
    playbackPositionMs: Float,
    lineStartTimeMs: Long,
    lineEndTimeMs: Long,
    graphemeCount: Int,
): WordByWordHighlightProgress {
    if (graphemeCount <= 0 || playbackPositionMs.isNaN()) {
        return WordByWordHighlightProgress(completedCount = 0, currentFraction = 0f)
    }
    if (playbackPositionMs <= lineStartTimeMs.toDouble()) {
        return WordByWordHighlightProgress(completedCount = 0, currentFraction = 0f)
    }
    if (playbackPositionMs >= lineEndTimeMs.toDouble()) {
        return WordByWordHighlightProgress(completedCount = graphemeCount, currentFraction = 0f)
    }

    val durationMs = lineEndTimeMs.toDouble() - lineStartTimeMs.toDouble()
    if (durationMs <= 0.0) {
        return WordByWordHighlightProgress(completedCount = 0, currentFraction = 0f)
    }
    val exactGraphemeCount =
        ((playbackPositionMs.toDouble() - lineStartTimeMs.toDouble()) / durationMs) * graphemeCount
    val completedCount = floor(exactGraphemeCount).toInt().coerceIn(0, graphemeCount)
    val currentFraction =
        if (completedCount == graphemeCount) {
            0f
        } else {
            (exactGraphemeCount - completedCount).toFloat().coerceIn(0f, 1f)
        }
    return WordByWordHighlightProgress(
        completedCount = completedCount,
        currentFraction = currentFraction,
    )
}

private class WordByWordLyricDrawCache(private val highlightEndOffsets: List<Int>) {
    private val completedPath = Path()
    private var graphemePaths: List<GraphemeHighlightPath> = emptyList()
    private var completedPathCount = -1

    fun updateLayout(layoutResult: TextLayoutResult) {
        var startOffset = 0
        graphemePaths =
            highlightEndOffsets.map { endOffset ->
                val path = layoutResult.getPathForRange(startOffset, endOffset)
                val direction = layoutResult.getBidiRunDirection(startOffset)
                startOffset = endOffset
                GraphemeHighlightPath(
                    path = path,
                    bounds = path.getBounds(),
                    isRightToLeft = direction == ResolvedTextDirection.Rtl,
                )
            }
        completedPathCount = -1
    }

    fun drawHighlight(drawScope: ContentDrawScope, progress: WordByWordHighlightProgress) = with(drawScope) {
        val completedCount = progress.completedCount.coerceIn(0, graphemePaths.size)
        if (completedCount > 0) {
            ensureCompletedPath(completedCount)
            clipPath(completedPath) {
                drawScope.drawContent()
            }
        }

        val currentGrapheme = graphemePaths.getOrNull(completedCount) ?: return@with
        val fraction = progress.currentFraction.coerceIn(0f, 1f)
        val bounds = currentGrapheme.bounds
        if (fraction <= 0f || bounds.width <= 0f || bounds.height <= 0f) return@with
        val revealWidth = bounds.width * fraction
        val revealLeft =
            if (currentGrapheme.isRightToLeft) {
                bounds.right - revealWidth
            } else {
                bounds.left
            }
        val revealRight =
            if (currentGrapheme.isRightToLeft) {
                bounds.right
            } else {
                bounds.left + revealWidth
            }
        clipRect(
            left = revealLeft,
            top = bounds.top,
            right = revealRight,
            bottom = bounds.bottom,
        ) {
            clipPath(currentGrapheme.path) {
                drawScope.drawContent()
            }
        }
    }

    private fun ensureCompletedPath(completedCount: Int) {
        if (completedPathCount == completedCount) return
        completedPath.reset()
        repeat(completedCount) { index ->
            completedPath.addPath(graphemePaths[index].path)
        }
        completedPathCount = completedCount
    }
}

private data class GraphemeHighlightPath(val path: Path, val bounds: Rect, val isRightToLeft: Boolean)
