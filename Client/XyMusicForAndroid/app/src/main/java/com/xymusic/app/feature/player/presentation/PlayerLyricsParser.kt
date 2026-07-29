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
    requireValidLyricsDocument(format, timing, content)
    val parsed =
        when (format) {
            LyricsFormat.PLAIN -> {
                require(timing == LyricsTiming.LINE) { "Plain lyrics must use line timing" }
                ParsedLrcLyrics(parsePlainLyrics(content), timing)
            }
            LyricsFormat.LRC -> parseLrcLyrics(content, timing)
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
    val lines =
        content
            .lineSequence()
            .flatMap { rawLine ->
                val lineTimes = LRC_TIMESTAMP_REGEX.findAll(rawLine).mapNotNull { it.toTimeMs() }.toList()
                if (lineTimes.isEmpty()) return@flatMap emptySequence()

                val lineContent = LRC_LINE_TAG_REGEX.replace(rawLine, "")
                val wordContent =
                    if (timing == LyricsTiming.WORD) {
                        parseWordContent(lineContent)
                    } else {
                        ParsedWordContent(stripEnhancedTimestamps(lineContent), emptyList())
                    }
                if (wordContent.text.isBlank()) return@flatMap emptySequence()

                lineTimes.asSequence().map { timeMs ->
                    PlayerLyricLineUi(
                        timeMs = timeMs,
                        text = wordContent.text,
                        words = wordContent.words,
                    )
                }
            }.sortedBy(PlayerLyricLineUi::timeMs)
            .toList()
    require(
        timing != LyricsTiming.WORD ||
            (lines.isNotEmpty() && lines.all { line -> line.words.isNotEmpty() }),
    ) { "Word-timed lyrics must contain word timestamps for every line" }
    return ParsedLrcLyrics(lines, timing)
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

private fun stripEnhancedTimestamps(content: String): String = ENHANCED_LRC_TIMESTAMP_REGEX.replace(content, "").trim()

private fun MatchResult.toTimeMs(): Long? =
    timestampToTimeMs(
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

private val LRC_TIMESTAMP_REGEX = Regex("\\[(\\d{1,3}):(\\d{2})(?:[.:](\\d{1,3}))?]")
private val LRC_LINE_TAG_REGEX = Regex("\\[[^]\\r\\n]*]")
private val ENHANCED_LRC_TIMESTAMP_REGEX = Regex("<\\d{1,3}:\\d{2}(?:[.:]\\d{1,3})?>")
private val WORD_TIMESTAMP_REGEX = Regex("<(\\d{1,3}):(\\d{2})(?:[.:](\\d{1,3}))?>")
