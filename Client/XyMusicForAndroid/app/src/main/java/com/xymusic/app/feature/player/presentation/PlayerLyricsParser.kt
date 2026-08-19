package com.xymusic.app.feature.player.presentation

import com.xymusic.app.core.model.media.LyricsFormat
import com.xymusic.app.core.model.media.LyricsTiming
import com.xymusic.app.core.model.media.requireValidLyricsDocument

internal data class ParsedPlayerLyrics(
    val lines: List<PlayerLyricLineUi>,
    val language: String?,
    val synchronized: Boolean,
    val timing: LyricsTiming,
) {
    fun currentLineIndex(positionMs: Long): Int = if (synchronized) playbackLyricIndex(lines, positionMs) else -1

    companion object {
        val Empty = ParsedPlayerLyrics(emptyList(), null, false, LyricsTiming.LINE)
    }
}

internal fun parsePlayerLyrics(
    content: String,
    format: LyricsFormat,
    timing: LyricsTiming,
    language: String?,
): ParsedPlayerLyrics {
    val normalizedContent = content
        .replace("\r\n", "\n")
        .replace('\r', '\n')
        .removePrefix(BYTE_ORDER_MARK)
    requireValidLyricsDocument(format, timing, normalizedContent)
    val parsed =
        when (format) {
            LyricsFormat.PLAIN -> {
                require(timing == LyricsTiming.LINE) { "Plain lyrics must use line timing" }
                ParsedLrcLyrics(parsePlainLyrics(normalizedContent), timing)
            }
            LyricsFormat.LRC -> parseLrcLyrics(normalizedContent, timing)
        }
    return ParsedPlayerLyrics(
        lines = parsed.lines,
        language = language,
        synchronized = format == LyricsFormat.LRC,
        timing = parsed.timing,
    )
}

private data class ParsedLrcLyrics(val lines: List<PlayerLyricLineUi>, val timing: LyricsTiming)

private data class ParsedWordContent(val text: String, val words: List<PlayerLyricWordUi>)

private fun parsePlainLyrics(content: String): List<PlayerLyricLineUi> = content
    .lineSequence()
    .map(String::trim)
    .filter(String::isNotBlank)
    .map { line -> PlayerLyricLineUi(timeMs = null, text = line) }
    .toList()

private fun parseLrcLyrics(content: String, timing: LyricsTiming): ParsedLrcLyrics {
    val offsetMs = parseLrcOffsetMs(content)
    val lines =
        content
            .lineSequence()
            .flatMap { rawLine ->
                val timestampPrefix = LRC_TIMESTAMP_PREFIX_REGEX.find(rawLine) ?: return@flatMap emptySequence()
                val lineTimes =
                    LRC_TIMESTAMP_REGEX
                        .findAll(timestampPrefix.value)
                        .mapNotNull { it.toTimeMs() }
                        .toList()
                if (lineTimes.isEmpty()) return@flatMap emptySequence()

                val lineContent =
                    LRC_LINE_TAG_REGEX.replace(
                        rawLine.substring(timestampPrefix.range.last + 1),
                        "",
                    )
                val wordContent =
                    if (timing == LyricsTiming.WORD) {
                        parseWordContent(lineContent)
                    } else {
                        ParsedWordContent(stripEnhancedTimestamps(lineContent), emptyList())
                    }
                if (wordContent.text.isBlank()) return@flatMap emptySequence()
                val baseLineTimeMs = lineTimes.first()

                lineTimes.asSequence().map { timeMs ->
                    val lineShiftMs = timeMs - baseLineTimeMs
                    PlayerLyricLineUi(
                        timeMs = applyLrcOffset(timeMs, offsetMs),
                        text = wordContent.text,
                        words = wordContent.words.map { word ->
                            word.copy(
                                timeMs = applyLrcTiming(word.timeMs, lineShiftMs, offsetMs),
                                endTimeMs = word.endTimeMs?.let { endTime ->
                                    applyLrcTiming(endTime, lineShiftMs, offsetMs)
                                },
                            )
                        },
                    )
                }
            }.sortedBy(PlayerLyricLineUi::timeMs)
            .toList()
    require(
        timing != LyricsTiming.WORD ||
            (lines.isNotEmpty() && lines.all { line -> line.words.isNotEmpty() }),
    ) { "Word-timed lyrics must contain word timestamps for every line" }
    return ParsedLrcLyrics(lines.withInferredFinalWordEnds(), timing)
}

private fun List<PlayerLyricLineUi>.withInferredFinalWordEnds(): List<PlayerLyricLineUi> = mapIndexed { index, line ->
    if (line.words.isEmpty() || line.words.last().endTimeMs != null) return@mapIndexed line
    val lastWord = line.words.last()
    val inferredEndTimeMs = lastWord.effectiveEndTimeMs()
    val nextLineTimeMs = getOrNull(index + 1)?.timeMs
    val boundedEndTimeMs =
        nextLineTimeMs?.let { nextTime ->
            minOf(inferredEndTimeMs, nextTime.coerceAtLeast(lastWord.timeMs))
        }
            ?: inferredEndTimeMs
    line.copy(
        words = line.words.dropLast(1) + lastWord.copy(endTimeMs = boundedEndTimeMs),
    )
}

private fun parseWordContent(lineContent: String): ParsedWordContent {
    val matches = WORD_TIMESTAMP_REGEX.findAll(lineContent).toList()
    if (matches.isEmpty()) return ParsedWordContent(stripEnhancedTimestamps(lineContent), emptyList())

    val wordTimes = matches.map { it.toTimeMs() }
    if (wordTimes.any { it == null }) {
        return ParsedWordContent(stripEnhancedTimestamps(lineContent), emptyList())
    }
    if (lineContent.substring(0, matches.first().range.first).isNotBlank()) {
        return ParsedWordContent(stripEnhancedTimestamps(lineContent), emptyList())
    }

    val words =
        matches.mapIndexedNotNull { index, match ->
            val segmentStart = match.range.last + 1
            val segmentEnd = matches.getOrNull(index + 1)?.range?.first ?: lineContent.length
            val text = lineContent.substring(segmentStart, segmentEnd)
            if (text.isEmpty()) {
                null
            } else {
                PlayerLyricWordUi(
                    timeMs = wordTimes[index]!!,
                    text = text,
                    endTimeMs = wordTimes.getOrNull(index + 1),
                )
            }
        }
    val text = words.joinToString(separator = "", transform = PlayerLyricWordUi::text)
    return if (words.isEmpty() || text.isBlank()) {
        ParsedWordContent(stripEnhancedTimestamps(lineContent), emptyList())
    } else {
        ParsedWordContent(text, words)
    }
}

private fun parseLrcOffsetMs(content: String): Long {
    val value = content.lineSequence()
        .filter(LRC_METADATA_ONLY_LINE_REGEX::matches)
        .flatMap { line -> LRC_OFFSET_TAG_REGEX.findAll(line) }
        .mapNotNull { match -> match.groupValues.getOrNull(1) }
        .firstOrNull()
        ?.toLongOrNull()
    return value ?: 0L
}

private fun applyLrcOffset(timeMs: Long, offsetMs: Long): Long = when {
    offsetMs > 0L -> timeMs.coerceAtMost(Long.MAX_VALUE - offsetMs) + offsetMs
    offsetMs == Long.MIN_VALUE -> 0L
    offsetMs < 0L -> (timeMs + offsetMs).coerceAtLeast(0L)
    else -> timeMs
}

private fun applyLrcTiming(timeMs: Long, lineShiftMs: Long, offsetMs: Long): Long =
    applyLrcOffset((timeMs + lineShiftMs).coerceAtLeast(0L), offsetMs)

private fun stripEnhancedTimestamps(content: String): String = ENHANCED_LRC_TIMESTAMP_REGEX.replace(content, "").trim()

private fun MatchResult.toTimeMs(): Long? = timestampToTimeMs(
    minutes = groupValues[1],
    seconds = groupValues[2],
    fraction = groupValues[3],
)

private fun timestampToTimeMs(minutes: String, seconds: String, fraction: String): Long? {
    val minuteValue = minutes.toLongOrNull() ?: return null
    val secondValue = seconds.toLongOrNull()?.takeIf { it in 0..59 } ?: return null
    val fractionMs =
        when (fraction.length) {
            0 -> 0L
            1 -> fraction.toLongOrNull()?.times(100)
            2 -> fraction.toLongOrNull()?.times(10)
            3 -> fraction.toLongOrNull()
            else -> null
        } ?: return null
    return (minuteValue * 60 + secondValue) * 1_000 + fractionMs
}

private val LRC_TIMESTAMP_PREFIX_REGEX =
    Regex("^\\s*(?:\\[\\d{1,3}:[0-5]\\d(?:[.:]\\d{1,3})?]\\s*)+")
private val LRC_TIMESTAMP_REGEX = Regex("\\[(\\d{1,3}):([0-5]\\d)(?:[.:](\\d{1,3}))?]")
private val LRC_LINE_TAG_REGEX = Regex("\\[[^]\\r\\n]*]")
private val LRC_METADATA_ONLY_LINE_REGEX =
    Regex("^\\s*(?:\\[[A-Za-z][A-Za-z0-9_-]*:[^\\[\\]\\r\\n]*]\\s*)+$")
private val LRC_OFFSET_TAG_REGEX = Regex("\\[offset:([+-]?\\d+)]", RegexOption.IGNORE_CASE)
private val ENHANCED_LRC_TIMESTAMP_REGEX = Regex("<\\d{1,3}:\\d{2}(?:[.:]\\d{1,3})?>")
private val WORD_TIMESTAMP_REGEX = Regex("<(\\d{1,3}):(\\d{2})(?:[.:](\\d{1,3}))?>")
private const val BYTE_ORDER_MARK = "\uFEFF"
