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
        if (rawLine.isBlank()) continue
        val prefix = LINE_TIMESTAMP_PREFIX_REGEX.find(rawLine)
        if (prefix == null) {
            if (METADATA_ONLY_LINE_REGEX.matches(rawLine)) continue
            return false
        }

        val body = rawLine.substring(prefix.range.last + 1).trim()
        if (body.isEmpty()) continue

        hasTimedLyricLine = true
        val remaining = WORD_TIMESTAMP_REGEX.replace(body, "")
        if (
            !WORD_TIMESTAMP_START_REGEX.containsMatchIn(body) ||
            ANY_WORD_MARKER_REGEX.containsMatchIn(remaining) ||
            remaining.isBlank() ||
            !wordTimestampsAreNondecreasing(body)
        ) {
            return false
        }
    }
    return hasTimedLyricLine
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

private val LINE_TIMESTAMP_PREFIX_REGEX =
    Regex("^\\s*(?:\\[[0-9]{1,3}:[0-5][0-9](?:[.:][0-9]{1,3})?]\\s*)+")
private val METADATA_ONLY_LINE_REGEX =
    Regex("^\\s*(?:\\[[A-Za-z][A-Za-z0-9_-]*:[^\\[\\]\\r\\n]*]\\s*)+$")
private val WORD_TIMESTAMP_REGEX = Regex("<([0-9]{1,3}):([0-5][0-9])(?:[.:]([0-9]{1,3}))?>")
private val WORD_TIMESTAMP_START_REGEX = Regex("^\\s*<[0-9]{1,3}:[0-5][0-9](?:[.:][0-9]{1,3})?>")
private val ANY_WORD_MARKER_REGEX = Regex("<[^>]*(?:>|$)")
private const val BYTE_ORDER_MARK = "\uFEFF"
