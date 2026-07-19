package com.xymusic.app.feature.player.presentation

import com.xymusic.app.core.model.media.LyricsFormat
import java.lang.Character.COMBINING_SPACING_MARK
import java.lang.Character.ENCLOSING_MARK
import java.lang.Character.NON_SPACING_MARK

internal data class ParsedPlayerLyrics(
    val lines: List<PlayerLyricLineUi>,
    val language: String?,
    val synchronized: Boolean,
) {
    fun currentLineIndex(positionMs: Long): Int = if (synchronized) playbackLyricIndex(lines, positionMs) else -1

    companion object {
        val Empty = ParsedPlayerLyrics(emptyList(), null, false)
    }
}

internal fun parsePlayerLyrics(content: String, format: LyricsFormat, language: String?): ParsedPlayerLyrics =
    ParsedPlayerLyrics(
        lines = when (format) {
            LyricsFormat.PLAIN -> parsePlainLyrics(content)
            LyricsFormat.LRC -> parseLrcLyrics(content)
        },
        language = language,
        synchronized = format == LyricsFormat.LRC,
    )

internal fun estimatedWordByWordLyricProgress(
    lines: List<PlayerLyricLineUi>,
    positionMs: Long,
    durationMs: Long,
): PlayerLyricProgressUi? {
    val lineIndex = playbackLyricIndex(lines, positionMs)
    if (lineIndex < 0) return null
    val line = lines[lineIndex]
    val startTimeMs = line.timeMs
    if (startTimeMs == null || line.highlightEndOffsets.isEmpty()) return null
    val endTimeMs = estimatedLineEndTimeMs(
        lines = lines,
        lineIndex = lineIndex,
        startTimeMs = startTimeMs,
        durationMs = durationMs,
    )
    if (positionMs !in startTimeMs until endTimeMs) return null
    val lineProgress =
        ((positionMs - startTimeMs).toDouble() / (endTimeMs - startTimeMs))
            .toFloat()
            .coerceIn(0f, 1f)
    val highlightedGraphemeCount =
        ((lineProgress * line.highlightEndOffsets.size).toInt() + 1)
            .coerceIn(1, line.highlightEndOffsets.size)
    val highlightedTextEndIndex = line.highlightEndOffsets[highlightedGraphemeCount - 1]
    return PlayerLyricProgressUi(
        lineIndex = lineIndex,
        highlightedTextEndIndex = highlightedTextEndIndex,
        lineEndTimeMs = endTimeMs,
        lineProgress = lineProgress,
    )
}

private fun parsePlainLyrics(content: String): List<PlayerLyricLineUi> = content
    .lineSequence()
    .map(String::trim)
    .filter(String::isNotBlank)
    .map { line -> PlayerLyricLineUi(timeMs = null, text = line) }
    .toList()

private fun parseLrcLyrics(content: String): List<PlayerLyricLineUi> = content
    .lineSequence()
    .flatMap { rawLine ->
        val text =
            ENHANCED_LRC_TIMESTAMP_REGEX
                .replace(LRC_LINE_TAG_REGEX.replace(rawLine, ""), "")
                .trim()
        if (text.isBlank()) return@flatMap emptySequence()
        val highlightEndOffsets = graphemeEndOffsets(text)
        LRC_TIMESTAMP_REGEX.findAll(rawLine).mapNotNull { match ->
            match.toTimeMs()?.let { timeMs ->
                PlayerLyricLineUi(
                    timeMs = timeMs,
                    text = text,
                    highlightEndOffsets = highlightEndOffsets,
                )
            }
        }
    }.sortedBy(PlayerLyricLineUi::timeMs)
    .toList()

private fun MatchResult.toTimeMs(): Long? {
    val minutes = groupValues[1].toLongOrNull() ?: return null
    val seconds = groupValues[2].toLongOrNull()?.takeIf { it in 0..59 } ?: return null
    val fractionText = groupValues[3]
    val fractionMs =
        when (fractionText.length) {
            0 -> 0L
            1 -> fractionText.toLongOrNull()?.times(100)
            2 -> fractionText.toLongOrNull()?.times(10)
            3 -> fractionText.toLongOrNull()
            else -> null
        } ?: return null
    return (minutes * 60 + seconds) * 1_000 + fractionMs
}

private fun estimatedLineEndTimeMs(
    lines: List<PlayerLyricLineUi>,
    lineIndex: Int,
    startTimeMs: Long,
    durationMs: Long,
): Long {
    val nextLineTimeMs = lines.getOrNull(lineIndex + 1)?.timeMs
    if (nextLineTimeMs != null && nextLineTimeMs > startTimeMs) return nextLineTimeMs
    val trackEndTimeMs = durationMs.takeIf { it > startTimeMs }
    val lastLineDurationMs =
        trackEndTimeMs
            ?.minus(startTimeMs)
            ?.coerceAtMost(MAX_LAST_LYRIC_LINE_DURATION_MS)
            ?.takeIf { it > 0 }
            ?: DEFAULT_LAST_LYRIC_LINE_DURATION_MS
    return startTimeMs + lastLineDurationMs
}

private fun graphemeEndOffsets(text: String): List<Int> = buildList {
    var clusterStart = 0
    while (clusterStart < text.length) {
        var clusterEnd = text.nextCodePointOffset(clusterStart)
        val firstCodePoint = text.codePointAt(clusterStart)
        if (firstCodePoint.isRegionalIndicator() && clusterEnd < text.length) {
            val nextCodePoint = text.codePointAt(clusterEnd)
            if (nextCodePoint.isRegionalIndicator()) {
                clusterEnd = text.nextCodePointOffset(clusterEnd)
            }
        }
        while (clusterEnd < text.length) {
            val codePoint = text.codePointAt(clusterEnd)
            when {
                codePoint.isGraphemeExtension() ->
                    clusterEnd = text.nextCodePointOffset(clusterEnd)
                codePoint == ZERO_WIDTH_JOINER && clusterEnd + 1 < text.length -> {
                    clusterEnd = text.nextCodePointOffset(clusterEnd)
                    clusterEnd = text.nextCodePointOffset(clusterEnd)
                }
                else -> break
            }
        }
        add(clusterEnd)
        clusterStart = clusterEnd
    }
}

private fun String.nextCodePointOffset(offset: Int): Int = offset + Character.charCount(codePointAt(offset))

private fun Int.isGraphemeExtension(): Boolean = Character.getType(this) in GRAPHEME_EXTENSION_TYPES ||
    this in EMOJI_MODIFIER_RANGE ||
    this in VARIATION_SELECTOR_RANGE ||
    this in SUPPLEMENTARY_VARIATION_SELECTOR_RANGE ||
    this in EMOJI_TAG_RANGE

private fun Int.isRegionalIndicator(): Boolean = this in REGIONAL_INDICATOR_RANGE

private val LRC_TIMESTAMP_REGEX = Regex("\\[(\\d{1,3}):(\\d{2})(?:[.:](\\d{1,3}))?]")
private val LRC_LINE_TAG_REGEX = Regex("\\[[^]\\r\\n]*]")
private val ENHANCED_LRC_TIMESTAMP_REGEX = Regex("<\\d{1,3}:\\d{2}(?:[.:]\\d{1,3})?>")
private val GRAPHEME_EXTENSION_TYPES =
    setOf(NON_SPACING_MARK.toInt(), COMBINING_SPACING_MARK.toInt(), ENCLOSING_MARK.toInt())
private val EMOJI_MODIFIER_RANGE = 0x1F3FB..0x1F3FF
private val VARIATION_SELECTOR_RANGE = 0xFE00..0xFE0F
private val SUPPLEMENTARY_VARIATION_SELECTOR_RANGE = 0xE0100..0xE01EF
private val EMOJI_TAG_RANGE = 0xE0020..0xE007F
private val REGIONAL_INDICATOR_RANGE = 0x1F1E6..0x1F1FF
private const val ZERO_WIDTH_JOINER = 0x200D
private const val DEFAULT_LAST_LYRIC_LINE_DURATION_MS = 6_000L
private const val MAX_LAST_LYRIC_LINE_DURATION_MS = 12_000L
