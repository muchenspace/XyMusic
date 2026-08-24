package com.xymusic.app.core.model.media

fun requireValidLyricsDocument(format: LyricsFormat, timing: LyricsTiming, content: String) {
    when (format) {
        LyricsFormat.PLAIN -> require(timing == LyricsTiming.LINE) {
            "Plain lyrics must use line timing"
        }
        LyricsFormat.LRC -> require((timing == LyricsTiming.WORD) == hasCompleteWordTiming(content)) {
            "Lyrics timing does not match lyrics content"
        }
    }
}

private fun hasCompleteWordTiming(content: String): Boolean {
    var hasTimedLyricLine = false
    val normalizedContent = content
        .replace("\r\n", "\n")
        .replace('\r', '\n')
        .removePrefix(BYTE_ORDER_MARK)
    for (rawLine in normalizedContent.lineSequence()) {
        when (rawLine.wordTimingStatus()) {
            LyricsLineTimingStatus.Ignored -> Unit
            LyricsLineTimingStatus.Timed -> hasTimedLyricLine = true
            LyricsLineTimingStatus.Invalid -> return false
        }
    }
    return hasTimedLyricLine
}

private fun String.wordTimingStatus(): LyricsLineTimingStatus {
    if (isBlank()) return LyricsLineTimingStatus.Ignored
    val prefix = LINE_TIMESTAMP_PREFIX_REGEX.find(this)
        ?: return if (METADATA_ONLY_LINE_REGEX.matches(this)) {
            LyricsLineTimingStatus.Ignored
        } else {
            LyricsLineTimingStatus.Invalid
        }
    val body = substring(prefix.range.last + 1).trim()
    if (body.isEmpty()) return LyricsLineTimingStatus.Ignored

    val remaining = WORD_TIMESTAMP_REGEX.replace(body, "")
    val valid =
        WORD_TIMESTAMP_START_REGEX.containsMatchIn(body) &&
            !ANY_WORD_MARKER_REGEX.containsMatchIn(remaining) &&
            remaining.isNotBlank() &&
            wordTimestampsAreNondecreasing(body)
    return if (valid) LyricsLineTimingStatus.Timed else LyricsLineTimingStatus.Invalid
}

private fun wordTimestampsAreNondecreasing(body: String): Boolean {
    var previousTimestampMs = -1L
    for (match in WORD_TIMESTAMP_REGEX.findAll(body)) {
        val timestampMs = match.toTimestampMs()
        if (timestampMs < previousTimestampMs) return false
        previousTimestampMs = timestampMs
    }
    return true
}

private fun MatchResult.toTimestampMs(): Long {
    val minutes = groupValues[1].toLong()
    val seconds = groupValues[2].toLong()
    val fractionMs = groupValues[3].padEnd(3, '0').toLong()
    return minutes * 60_000 + seconds * 1_000 + fractionMs
}

private enum class LyricsLineTimingStatus {
    Ignored,
    Timed,
    Invalid,
}

private val LINE_TIMESTAMP_PREFIX_REGEX =
    Regex("^\\s*(?:\\[[0-9]{1,3}:[0-5][0-9](?:[.:][0-9]{1,3})?]\\s*)+")
private val METADATA_ONLY_LINE_REGEX =
    Regex("^\\s*(?:\\[[A-Za-z][A-Za-z0-9_-]*:[^\\[\\]\\r\\n]*]\\s*)+$")
private val WORD_TIMESTAMP_REGEX = Regex("<([0-9]{1,3}):([0-5][0-9])(?:[.:]([0-9]{1,3}))?>")
private val WORD_TIMESTAMP_START_REGEX = Regex("^\\s*<[0-9]{1,3}:[0-5][0-9](?:[.:][0-9]{1,3})?>")
private val ANY_WORD_MARKER_REGEX = Regex("<[^>]*(?:>|$)")
private const val BYTE_ORDER_MARK = "\uFEFF"
