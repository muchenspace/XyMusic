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

@Composable
internal fun WordByWordLyricText(
    text: String,
    words: List<PlayerLyricWordUi>,
    playbackPosition: State<Float>,
    modifier: Modifier = Modifier,
    baseColor: Color,
    highlightColor: Color,
    style: TextStyle,
) {
    val normalizedWords =
        remember(text, words) {
            var endOffset = 0
            words.mapNotNull { word ->
                if (word.text.isEmpty()) {
                    null
				} else {
					val nextEndOffset = endOffset + word.text.length
					if (nextEndOffset > text.length) {
						null
					} else {
						endOffset = nextEndOffset
						TimedWordLayout(word.timeMs, word.endTimeMs, nextEndOffset)
					}
				}
            }
        }
    val drawCache = remember(text, normalizedWords) { WordByWordLyricDrawCache(normalizedWords) }

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
                        drawCache.drawHighlight(
                            this,
                            calculateWordTimedHighlightProgressForLayouts(
                                words = normalizedWords,
                                playbackPositionMs = playbackPosition.value,
                            ),
                        )
                    },
            color = highlightColor,
            style = style,
            onTextLayout = drawCache::updateLayout,
        )
    }
}

internal data class WordTimedHighlightProgress(val completedCount: Int, val currentFraction: Float)

internal fun calculateWordTimedHighlightProgress(
	words: List<PlayerLyricWordUi>,
	playbackPositionMs: Long,
): WordTimedHighlightProgress = calculateWordTimedHighlightProgressByTimes(
	wordTimes = words.map { word -> WordTiming(word.timeMs, word.endTimeMs) },
	playbackPositionMs = playbackPositionMs.toFloat(),
)

private fun calculateWordTimedHighlightProgressForLayouts(
    words: List<TimedWordLayout>,
    playbackPositionMs: Float,
): WordTimedHighlightProgress {
    if (words.isEmpty() || playbackPositionMs.isNaN()) {
        return WordTimedHighlightProgress(completedCount = 0, currentFraction = 0f)
    }
    var completedCount = 0
    for (word in words) {
        if (playbackPositionMs < word.timeMs) {
            return WordTimedHighlightProgress(completedCount, 0f)
        }
        val endTimeMs = word.endTimeMs
        if (endTimeMs == null) {
            return WordTimedHighlightProgress(completedCount, 0f)
        }
        if (endTimeMs <= word.timeMs || playbackPositionMs >= endTimeMs) {
            completedCount += 1
            continue
        }
        val fraction =
            ((playbackPositionMs - word.timeMs).toDouble() / (endTimeMs - word.timeMs))
                .toFloat()
                .coerceIn(0f, 1f)
        return WordTimedHighlightProgress(completedCount, fraction)
    }
    return WordTimedHighlightProgress(completedCount, 0f)
}

private fun calculateWordTimedHighlightProgressByTimes(
	wordTimes: List<WordTiming>,
	playbackPositionMs: Float,
): WordTimedHighlightProgress {
	if (wordTimes.isEmpty() || playbackPositionMs.isNaN()) {
		return WordTimedHighlightProgress(completedCount = 0, currentFraction = 0f)
	}
	var completedCount = 0
	for (word in wordTimes) {
		if (playbackPositionMs < word.timeMs) {
			return WordTimedHighlightProgress(completedCount, 0f)
		}
		val endTimeMs = word.endTimeMs
		if (endTimeMs == null) {
			return WordTimedHighlightProgress(completedCount, 0f)
		}
		if (endTimeMs <= word.timeMs) {
			completedCount += 1
			continue
		}
		if (playbackPositionMs >= endTimeMs) {
			completedCount += 1
			continue
		}
		val fraction =
			((playbackPositionMs - word.timeMs).toDouble() / (endTimeMs - word.timeMs))
				.toFloat()
				.coerceIn(0f, 1f)
		return WordTimedHighlightProgress(completedCount, fraction)
	}
	return WordTimedHighlightProgress(completedCount, 0f)
}

private data class WordTiming(val timeMs: Long, val endTimeMs: Long?)

private data class TimedWordLayout(val timeMs: Long, val endTimeMs: Long?, val endOffset: Int)

private class WordByWordLyricDrawCache(private val words: List<TimedWordLayout>) {
    private val completedPath = Path()
    private var wordPaths: List<WordHighlightPath> = emptyList()
    private var completedPathCount = -1

    fun updateLayout(layoutResult: TextLayoutResult) {
        var startOffset = 0
        wordPaths =
            words.map { word ->
                val path = layoutResult.getPathForRange(startOffset, word.endOffset)
                val direction = layoutResult.getBidiRunDirection(startOffset)
                startOffset = word.endOffset
                WordHighlightPath(
                    path = path,
                    bounds = path.getBounds(),
                    isRightToLeft = direction == ResolvedTextDirection.Rtl,
                )
            }
        completedPathCount = -1
    }

    fun drawHighlight(drawScope: ContentDrawScope, progress: WordTimedHighlightProgress) = with(drawScope) {
        val completedCount = progress.completedCount.coerceIn(0, wordPaths.size)
        if (completedCount >= wordPaths.size && wordPaths.isNotEmpty()) {
            drawScope.drawContent()
            return@with
        }
        if (completedCount > 0) {
            ensureCompletedPath(completedCount)
            clipPath(completedPath) {
                drawScope.drawContent()
            }
        }

        val currentWord = wordPaths.getOrNull(completedCount) ?: return@with
        val fraction = progress.currentFraction.coerceIn(0f, 1f)
        val bounds = currentWord.bounds
        if (fraction <= 0f || bounds.width <= 0f || bounds.height <= 0f) return@with
        val revealWidth = bounds.width * fraction
        val revealLeft =
            if (currentWord.isRightToLeft) {
                bounds.right - revealWidth
            } else {
                bounds.left
            }
        val revealRight =
            if (currentWord.isRightToLeft) {
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
            clipPath(currentWord.path) {
                drawScope.drawContent()
            }
        }
    }

    private fun ensureCompletedPath(completedCount: Int) {
        if (completedPathCount == completedCount) return
        completedPath.reset()
        repeat(completedCount) { index ->
            completedPath.addPath(wordPaths[index].path)
        }
        completedPathCount = completedCount
    }
}

private data class WordHighlightPath(val path: Path, val bounds: Rect, val isRightToLeft: Boolean)
