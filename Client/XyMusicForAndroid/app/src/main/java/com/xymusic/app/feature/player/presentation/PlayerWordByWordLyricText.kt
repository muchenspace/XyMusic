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
import androidx.compose.ui.text.style.TextOverflow
import kotlin.math.abs
import kotlin.math.min

@Composable
internal fun WordByWordLyricText(
    text: String,
    words: List<PlayerLyricWordUi>,
    playbackPosition: State<Float>,
    modifier: Modifier = Modifier,
    isActive: Boolean = true,
    lineIndex: Int = if (isActive) 0 else 1,
    currentLyricIndex: Int = 0,
    lineEmphasis: State<Float>? = null,
    baseColor: Color,
    highlightColor: Color,
    style: TextStyle,
) {
    val normalizedWords =
        remember(text, words) { normalizedTimedWordLayouts(text, words) }
    val drawCache = remember(text, normalizedWords) { WordByWordLyricDrawCache(normalizedWords) }

    Box(modifier = modifier) {
        Text(
            text = text,
            modifier = Modifier
                .drawWithContent {
                    drawContent()
                    // The line clock is also the visibility gate.  Starting a word overlay at
                    // the exact timestamp makes a newly-entering wrapped line flash before its
                    // scroll transition has settled.  Reading the state from the draw phase keeps
                    // this high-frequency update out of composition and layout.
                    val emphasis =
                        wordByWordHighlightEmphasis(
                            isActive = lineIndex == currentLyricIndex,
                            lineEmphasis = lineEmphasis?.value,
                        )
                            .smoothLyricEmphasis()
                    val highlightPosition =
                        wordByWordHighlightPlaybackPosition(
                            lineIndex = lineIndex,
                            currentLyricIndex = currentLyricIndex,
                            playbackPositionMs = playbackPosition.value,
                        )
                    if (highlightPosition != null && emphasis > 0f) {
                        drawCache.drawHighlight(
                            drawScope = this,
                            playbackPositionMs = highlightPosition,
                            highlightColor = highlightColor,
                            alpha = emphasis,
                        )
                    }
                },
            color = baseColor,
            style = style,
            softWrap = true,
            overflow = TextOverflow.Clip,
            maxLines = Int.MAX_VALUE,
            onTextLayout = drawCache::updateLayout,
        )
    }
}

/**
 * Converts the line-position interpolation into a gentle alpha curve.  A cubic smoothstep has
 * zero slope at both ends, so a line never appears as a one-frame flash when a frame lands exactly
 * on a lyric boundary.  The function is intentionally allocation-free and is called from draw.
 */
private fun Float.smoothLyricEmphasis(): Float {
    val value = coerceIn(0f, 1f)
    return value * value * (3f - 2f * value)
}

internal fun wordByWordHighlightEmphasis(isActive: Boolean, lineEmphasis: Float?): Float =
    lineEmphasis ?: if (isActive) 1f else 0f

internal fun wordByWordHighlightPlaybackPosition(
    lineIndex: Int,
    currentLyricIndex: Int,
    playbackPositionMs: Float,
): Float? = playbackPositionMs.takeIf { lineIndex <= currentLyricIndex }

internal data class WordTimedHighlightProgress(val completedCount: Int, val currentFraction: Float)

internal fun calculateWordTimedHighlightProgress(
    words: List<PlayerLyricWordUi>,
    playbackPositionMs: Long,
): WordTimedHighlightProgress {
    val indexedWords = words.mapIndexed { index, word ->
        TimedWordLayout(
            timeMs = word.timeMs,
            endTimeMs = word.effectiveEndTimeMs(words.getOrNull(index + 1)?.timeMs),
            startOffset = 0,
            endOffset = 0,
        )
    }
    val output = MutableWordTimedHighlightProgress()
    WordTimingIndex(indexedWords).progressInto(playbackPositionMs.toFloat(), output)
    return WordTimedHighlightProgress(output.completedCount, output.currentFraction)
}

internal data class TimedWordLayout(val timeMs: Long, val endTimeMs: Long?, val startOffset: Int, val endOffset: Int)

/**
 * Text layout ranges use UTF-16 offsets. Validate each word against the
 * original string so highlighting never drifts when a word contains spaces,
 * surrogate pairs, or combining characters.
 */
internal fun normalizedTimedWordLayouts(text: String, words: List<PlayerLyricWordUi>): List<TimedWordLayout> {
    val validBoundaries = BooleanArray(text.length + 1)
    validBoundaries[0] = true
    characterFragmentOffsets(text, 0, text.length).forEach { (_, endOffset) ->
        validBoundaries[endOffset] = true
    }
    var startOffset = 0
    val layouts = words.mapIndexedNotNull { index, word ->
        if (word.text.isEmpty()) {
            null
        } else {
            val endOffset = startOffset + word.text.length
            if (
                endOffset > text.length ||
                !validBoundaries[startOffset] ||
                !validBoundaries[endOffset] ||
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
                endTimeMs = word.effectiveEndTimeMs(words.getOrNull(index + 1)?.timeMs),
                startOffset = startOffset,
                endOffset = endOffset,
            ).also { startOffset = endOffset }
        }
    }
    return layouts.takeIf { startOffset == text.length } ?: emptyList()
}

private class WordByWordLyricDrawCache(private val words: List<TimedWordLayout>) {
    private val completedPath = Path()
    private val frameHighlightPath = Path()
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
                    fragments = splitIntoCharacterFragments(layoutResult, word.startOffset, word.endOffset),
                )
            }
        completedPathCount = -1
    }

    fun drawHighlight(drawScope: ContentDrawScope, playbackPositionMs: Float, highlightColor: Color, alpha: Float) {
        if (alpha <= 0f) return
        timingIndex.progressInto(playbackPositionMs, progress)
        with(drawScope) {
            val layout = layoutResult ?: return@with
            val completedCount = progress.completedCount.coerceIn(0, wordPaths.size)
            if (completedCount >= wordPaths.size && wordPaths.isNotEmpty()) {
                drawText(layout, color = highlightColor, alpha = alpha.coerceIn(0f, 1f))
            } else {
                drawPartialHighlight(
                    layout = layout,
                    completedCount = completedCount,
                    word = wordPaths.getOrNull(completedCount),
                    fraction = progress.currentFraction,
                    highlightColor = highlightColor,
                    alpha = alpha,
                )
            }
        }
    }

    private fun ContentDrawScope.drawPartialHighlight(
        layout: TextLayoutResult,
        completedCount: Int,
        word: WordHighlightPath?,
        fraction: Float,
        highlightColor: Color,
        alpha: Float,
    ) {
        ensureCompletedPath(completedCount)
        frameHighlightPath.reset()
        var hasCompleteFragments = completedCount > 0
        if (hasCompleteFragments) frameHighlightPath.addPath(completedPath)
        val currentWord = word
        if (currentWord == null) {
            if (hasCompleteFragments) {
                clipPath(frameHighlightPath) {
                    drawText(layout, color = highlightColor, alpha = alpha.coerceIn(0f, 1f))
                }
            }
            return
        }
        val clampedFraction = fraction.coerceIn(0f, 1f)
        if (clampedFraction <= 0f) {
            if (hasCompleteFragments) {
                clipPath(frameHighlightPath) {
                    drawText(layout, color = highlightColor, alpha = alpha.coerceIn(0f, 1f))
                }
            }
            return
        }
        val totalAdvance = currentWord.totalAdvance
        if (totalAdvance <= 0f) {
            if (hasCompleteFragments) {
                clipPath(frameHighlightPath) {
                    drawText(layout, color = highlightColor, alpha = alpha.coerceIn(0f, 1f))
                }
            }
            return
        }
        var remainingAdvance = totalAdvance * clampedFraction
        var partialFragment: WordHighlightFragment? = null
        var partialAdvance = 0f
        for (fragment in currentWord.fragments) {
            if (remainingAdvance >= fragment.advanceWidth) {
                frameHighlightPath.addPath(fragment.path)
                hasCompleteFragments = true
                remainingAdvance -= fragment.advanceWidth
            } else {
                if (remainingAdvance > 0f) {
                    partialFragment = fragment
                    partialAdvance = remainingAdvance
                }
                break
            }
        }
        if (hasCompleteFragments) {
            clipPath(frameHighlightPath) {
                drawText(layout, color = highlightColor, alpha = alpha.coerceIn(0f, 1f))
            }
        }
        partialFragment?.let { fragment ->
            drawFragmentHighlight(layout, fragment, partialAdvance, highlightColor, alpha)
        }
    }

    private fun ContentDrawScope.drawFragmentHighlight(
        layout: TextLayoutResult,
        fragment: WordHighlightFragment,
        remainingAdvance: Float,
        highlightColor: Color,
        alpha: Float,
    ) {
        if (remainingAdvance <= 0f || fragment.bounds.width <= 0f || fragment.advanceWidth <= 0f) return
        val revealFraction = (remainingAdvance / fragment.advanceWidth).coerceIn(0f, 1f)
        val revealWidth = min(fragment.bounds.width * revealFraction, fragment.bounds.width)
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

    /**
     * Keeps each timed word's reveal range independent. A single range that crosses a soft wrap
     * can produce a path whose bounds cover the whole visual line, so it must not be used as the
     * unit of the first visible animation step.
     */
    private fun splitIntoCharacterFragments(
        layoutResult: TextLayoutResult,
        startOffset: Int,
        endOffset: Int,
    ): List<WordHighlightFragment> {
        if (startOffset >= endOffset) return emptyList()
        val fragments = mutableListOf<WordHighlightFragment>()
        characterFragmentOffsets(
            text = layoutResult.layoutInput.text.text,
            startOffset = startOffset,
            endOffset = endOffset,
        ).forEach { (fragmentStart, fragmentEnd) ->
            val path = layoutResult.getPathForRange(fragmentStart, fragmentEnd)
            val bounds = characterBounds(layoutResult, fragmentStart, fragmentEnd, path)
            val advanceWidth = characterAdvanceWidth(layoutResult, fragmentStart, fragmentEnd, bounds)
            fragments +=
                WordHighlightFragment(
                    path = path,
                    bounds = bounds,
                    isRightToLeft = layoutResult.getBidiRunDirection(fragmentStart) == ResolvedTextDirection.Rtl,
                    advanceWidth = advanceWidth,
                )
        }
        return fragments
    }

    private fun characterAdvanceWidth(
        layoutResult: TextLayoutResult,
        startOffset: Int,
        endOffset: Int,
        bounds: Rect,
    ): Float {
        val startLine = layoutResult.getLineForOffset(startOffset)
        val endLine = layoutResult.getLineForOffset((endOffset - 1).coerceAtLeast(startOffset))
        if (startLine != endLine || endOffset >= layoutResult.getLineEnd(startLine)) return bounds.width
        val startPosition = layoutResult.getHorizontalPosition(startOffset, usePrimaryDirection = true)
        val endPosition = layoutResult.getHorizontalPosition(endOffset, usePrimaryDirection = true)
        return maxOf(abs(endPosition - startPosition), bounds.width)
    }

    private fun characterBounds(layoutResult: TextLayoutResult, startOffset: Int, endOffset: Int, path: Path): Rect {
        var bounds: Rect? = null
        var offset = startOffset
        while (offset < endOffset) {
            val characterBounds = layoutResult.getBoundingBox(offset)
            if (characterBounds.width > 0f && characterBounds.height > 0f) {
                bounds = bounds?.let { unionRects(it, characterBounds) } ?: characterBounds
            }
            offset += Character.charCount(layoutResult.layoutInput.text.text.codePointAt(offset))
        }
        return bounds ?: path.getBounds()
    }

    private fun unionRects(first: Rect, second: Rect): Rect = Rect(
        left = minOf(first.left, second.left),
        top = minOf(first.top, second.top),
        right = maxOf(first.right, second.right),
        bottom = maxOf(first.bottom, second.bottom),
    )
}

/** Keeps a timed word's visual fragments inside its own UTF-16 range, even when it soft-wraps. */
internal fun characterFragmentOffsets(text: String, startOffset: Int, endOffset: Int): List<Pair<Int, Int>> {
    if (!text.hasValidFragmentRange(startOffset, endOffset)) return emptyList()
    val fragments = mutableListOf<Pair<Int, Int>>()
    var fragmentStart = startOffset
    while (fragmentStart < endOffset) {
        val fragmentEnd = text.nextLyricCharacterFragmentEnd(fragmentStart, endOffset)
        if (fragmentEnd <= fragmentStart) break
        fragments += fragmentStart to fragmentEnd
        fragmentStart = fragmentEnd
    }
    return fragments
}

private fun String.hasValidFragmentRange(startOffset: Int, endOffset: Int): Boolean = startOffset < endOffset &&
    startOffset >= 0 &&
    endOffset <= length &&
    isUtf16Boundary(startOffset) &&
    isUtf16Boundary(endOffset)

private fun String.isUtf16Boundary(offset: Int): Boolean = offset in 0..length &&
    !(offset > 0 && offset < length && this[offset - 1].isHighSurrogate() && this[offset].isLowSurrogate())

/** Uses the text layout's line boundaries so a wrap never splits the last character of a line. */
internal fun lineFragmentOffsets(
    startOffset: Int,
    endOffset: Int,
    lineForOffset: (Int) -> Int,
    lineEnd: (Int) -> Int,
): List<Pair<Int, Int>> {
    if (startOffset >= endOffset) return emptyList()
    val fragments = mutableListOf<Pair<Int, Int>>()
    var fragmentStart = startOffset
    while (fragmentStart < endOffset) {
        val fragmentEnd = lineEnd(lineForOffset(fragmentStart)).coerceAtMost(endOffset)
        if (fragmentEnd <= fragmentStart) break
        fragments += fragmentStart to fragmentEnd
        fragmentStart = fragmentEnd
    }
    return fragments
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
    val totalAdvance: Float = fragments.sumOf { fragment -> fragment.advanceWidth.toDouble() }.toFloat()
}

private data class WordHighlightFragment(
    val path: Path,
    val bounds: Rect,
    val isRightToLeft: Boolean,
    val advanceWidth: Float,
)
