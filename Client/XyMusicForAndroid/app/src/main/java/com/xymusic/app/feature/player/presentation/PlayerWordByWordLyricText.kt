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
import androidx.compose.ui.text.TextLayoutResult
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.drawText
import androidx.compose.ui.text.style.ResolvedTextDirection
import kotlin.math.min

@Composable
internal fun WordByWordLyricText(
    text: String,
    words: List<PlayerLyricWordUi>,
    playbackPosition: State<Float>,
    modifier: Modifier = Modifier,
    isActive: Boolean = true,
    lineEmphasis: State<Float>? = null,
    baseColor: Color,
    highlightColor: Color,
    style: TextStyle,
) {
    val normalizedWords =
        remember(text, words) { normalizedTimedWordLayouts(text, words) }
    val drawCache = remember(text, normalizedWords) { WordByWordLyricDrawCache(text, normalizedWords) }

    Box(modifier = modifier) {
        Text(
            text = text,
            modifier = Modifier.drawWithContent {
                drawContent()
                val emphasis = lineEmphasis?.value ?: if (isActive) 1f else 0f
                if (emphasis > 0f) {
                    drawCache.drawHighlight(
                        drawScope = this,
                        playbackPositionMs = playbackPosition.value,
                        highlightColor = highlightColor,
                        alpha = emphasis,
                    )
                }
            },
            color = baseColor,
            style = style,
            softWrap = true,
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
            return WordTimedHighlightProgress(completedCount, 1f)
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

private data class WordTiming(val timeMs: Long, val endTimeMs: Long?)

internal data class TimedWordLayout(val timeMs: Long, val endTimeMs: Long?, val startOffset: Int, val endOffset: Int)

/**
 * Text layout ranges use UTF-16 offsets. Validate each word against the
 * original string so highlighting never drifts when a word contains spaces,
 * surrogate pairs, or combining characters.
 */
internal fun normalizedTimedWordLayouts(text: String, words: List<PlayerLyricWordUi>): List<TimedWordLayout> {
    var startOffset = 0
    val layouts = words.mapNotNull { word ->
        if (word.text.isEmpty()) {
            null
        } else {
            val endOffset = startOffset + word.text.length
            if (
                endOffset > text.length ||
                !text.regionMatches(
                    thisOffset = startOffset,
                    other = word.text,
                    otherOffset = 0,
                    length = word.text.length,
                )
            ) {
                return emptyList()
            }
            TimedWordLayout(
                timeMs = word.timeMs,
                endTimeMs = word.endTimeMs,
                startOffset = startOffset,
                endOffset = endOffset,
            ).also { startOffset = endOffset }
        }
    }
    return layouts.takeIf { startOffset == text.length } ?: emptyList()
}

private class WordByWordLyricDrawCache(private val text: String, private val words: List<TimedWordLayout>) {
    private val completedPath = Path()
    private val timingIndex = WordTimingIndex(words)
    private val progress = MutableWordTimedHighlightProgress()
    private var layoutResult: TextLayoutResult? = null
    private var wordPaths: List<WordHighlightPath> = emptyList()
    private var completedPathCount = -1

    fun updateLayout(layoutResult: TextLayoutResult) {
        if (this.layoutResult === layoutResult) return
        this.layoutResult = layoutResult
        wordPaths =
            words.map { word ->
                WordHighlightPath(
                    fragments = splitIntoLineFragments(layoutResult, word.startOffset, word.endOffset),
                )
            }
        completedPathCount = -1
    }

    fun drawHighlight(drawScope: ContentDrawScope, playbackPositionMs: Float, highlightColor: Color, alpha: Float) {
        timingIndex.progressInto(playbackPositionMs, progress)
        with(drawScope) {
            val layout = layoutResult ?: return@with
            val completedCount = progress.completedCount.coerceIn(0, wordPaths.size)
            if (completedCount >= wordPaths.size && wordPaths.isNotEmpty()) {
                drawText(layout, color = highlightColor, alpha = alpha)
            } else {
                drawCompletedHighlight(layout, completedCount, highlightColor, alpha)
                drawCurrentHighlight(
                    layout = layout,
                    word = wordPaths.getOrNull(completedCount),
                    fraction = progress.currentFraction,
                    highlightColor = highlightColor,
                    alpha = alpha,
                )
            }
        }
    }

    private fun ContentDrawScope.drawCompletedHighlight(
        layout: TextLayoutResult,
        completedCount: Int,
        highlightColor: Color,
        alpha: Float,
    ) {
        if (completedCount <= 0) return
        ensureCompletedPath(completedCount)
        clipPath(completedPath) {
            drawText(layout, color = highlightColor, alpha = alpha)
        }
    }

    private fun ContentDrawScope.drawCurrentHighlight(
        layout: TextLayoutResult,
        word: WordHighlightPath?,
        fraction: Float,
        highlightColor: Color,
        alpha: Float,
    ) {
        val currentWord = word ?: return
        val clampedFraction = fraction.coerceIn(0f, 1f)
        if (clampedFraction <= 0f) return
        val totalWidth = currentWord.totalWidth
        if (totalWidth <= 0f) return
        var remainingWidth = totalWidth * clampedFraction
        currentWord.fragments.forEach { fragment ->
            drawFragmentHighlight(layout, fragment, remainingWidth, highlightColor, alpha)
            remainingWidth -= fragment.bounds.width
        }
    }

    private fun ContentDrawScope.drawFragmentHighlight(
        layout: TextLayoutResult,
        fragment: WordHighlightFragment,
        remainingWidth: Float,
        highlightColor: Color,
        alpha: Float,
    ) {
        if (remainingWidth <= 0f || fragment.bounds.width <= 0f) return
        val revealWidth = min(remainingWidth, fragment.bounds.width)
        val revealLeft =
            if (fragment.isRightToLeft) {
                fragment.bounds.right - revealWidth
            } else {
                fragment.bounds.left
            }
        val revealRight =
            if (fragment.isRightToLeft) {
                fragment.bounds.right
            } else {
                fragment.bounds.left + revealWidth
            }
        clipRect(
            left = revealLeft,
            top = fragment.bounds.top,
            right = revealRight,
            bottom = fragment.bounds.bottom,
        ) {
            clipPath(fragment.path) {
                drawText(layout, color = highlightColor, alpha = alpha)
            }
        }
    }

    private fun ensureCompletedPath(completedCount: Int) {
        if (completedPathCount == completedCount) return
        completedPath.reset()
        repeat(completedCount) { index ->
            wordPaths[index].fragments.forEach { fragment -> completedPath.addPath(fragment.path) }
        }
        completedPathCount = completedCount
    }

    private fun splitIntoLineFragments(
        layoutResult: TextLayoutResult,
        startOffset: Int,
        endOffset: Int,
    ): List<WordHighlightFragment> {
        if (startOffset >= endOffset) return emptyList()
        val fragments = mutableListOf<WordHighlightFragment>()
        var fragmentStart = startOffset
        while (fragmentStart < endOffset) {
            val line = layoutResult.getLineForOffset(fragmentStart)
            var fragmentEnd = fragmentStart + Character.charCount(text.codePointAt(fragmentStart))
            while (fragmentEnd < endOffset) {
                val nextOffset =
                    fragmentEnd + Character.charCount(text.codePointAt(fragmentEnd))
                if (nextOffset >= endOffset || layoutResult.getLineForOffset(nextOffset) == line) {
                    fragmentEnd = nextOffset
                } else {
                    break
                }
            }
            val path = layoutResult.getPathForRange(fragmentStart, fragmentEnd)
            fragments +=
                WordHighlightFragment(
                    path = path,
                    bounds = path.getBounds(),
                    isRightToLeft = layoutResult.getBidiRunDirection(fragmentStart) == ResolvedTextDirection.Rtl,
                )
            fragmentStart = fragmentEnd
        }
        return fragments
    }
}

private class MutableWordTimedHighlightProgress {
    var completedCount: Int = 0
    var currentFraction: Float = 0f

    fun set(completedCount: Int, currentFraction: Float) {
        this.completedCount = completedCount
        this.currentFraction = currentFraction
    }
}

private class WordTimingIndex(private val words: List<TimedWordLayout>) {
    private val completionTimes = LongArray(words.size) { index ->
        words[index].endTimeMs ?: Long.MAX_VALUE
    }
    private val completionTimesAreSorted = completionTimesAreSorted(completionTimes)

    fun progressInto(playbackPositionMs: Float, output: MutableWordTimedHighlightProgress) {
        when {
            words.isEmpty() || playbackPositionMs.isNaN() -> output.set(0, 0f)
            !completionTimesAreSorted -> calculateLinearProgress(playbackPositionMs, output)
            else -> calculateSortedProgress(playbackPositionMs, output)
        }
    }

    private fun calculateSortedProgress(playbackPositionMs: Float, output: MutableWordTimedHighlightProgress) {
        val completedCount = firstCompletionAfter(playbackPositionMs)
        val currentWord = words.getOrNull(completedCount)
        when {
            currentWord == null -> output.set(completedCount, 0f)
            playbackPositionMs < currentWord.timeMs -> output.set(completedCount, 0f)
            currentWord.endTimeMs == null -> output.set(completedCount, 1f)
            else -> progressForTimedWord(currentWord, completedCount, playbackPositionMs, output)
        }
    }

    private fun progressForTimedWord(
        word: TimedWordLayout,
        completedCount: Int,
        playbackPositionMs: Float,
        output: MutableWordTimedHighlightProgress,
    ) {
        val endTimeMs = word.endTimeMs ?: return output.set(completedCount, 1f)
        val duration = endTimeMs - word.timeMs
        if (duration <= 0L || playbackPositionMs >= endTimeMs) {
            output.set(completedCount + 1, 0f)
            return
        }
        val fraction =
            ((playbackPositionMs - word.timeMs).toDouble() / duration)
                .toFloat()
                .coerceIn(0f, 1f)
        output.set(completedCount, fraction)
    }

    private fun firstCompletionAfter(playbackPositionMs: Float): Int {
        var low = 0
        var high = completionTimes.lastIndex
        while (low <= high) {
            val middle = (low + high).ushr(1)
            if (completionTimes[middle].toFloat() <= playbackPositionMs) {
                low = middle + 1
            } else {
                high = middle - 1
            }
        }
        return low
    }

    private fun calculateLinearProgress(playbackPositionMs: Float, output: MutableWordTimedHighlightProgress) {
        var completedCount = 0
        for (word in words) {
            if (playbackPositionMs < word.timeMs) {
                output.set(completedCount, 0f)
                return
            }
            val endTimeMs = word.endTimeMs
                ?: return output.set(completedCount, 1f)
            if (endTimeMs <= word.timeMs || playbackPositionMs >= endTimeMs) {
                completedCount += 1
                continue
            }
            val fraction =
                ((playbackPositionMs - word.timeMs).toDouble() / (endTimeMs - word.timeMs))
                    .toFloat()
                    .coerceIn(0f, 1f)
            output.set(completedCount, fraction)
            return
        }
        output.set(completedCount, 0f)
    }

    private fun completionTimesAreSorted(times: LongArray): Boolean {
        for (index in 1 until times.size) {
            if (times[index - 1] > times[index]) return false
        }
        return true
    }
}

private class WordHighlightPath(val fragments: List<WordHighlightFragment>) {
    val totalWidth: Float = fragments.sumOf { fragment -> fragment.bounds.width.toDouble() }.toFloat()
}

private data class WordHighlightFragment(val path: Path, val bounds: Rect, val isRightToLeft: Boolean)
