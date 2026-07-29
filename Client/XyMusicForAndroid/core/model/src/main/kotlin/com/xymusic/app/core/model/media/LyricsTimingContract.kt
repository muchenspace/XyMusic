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
    for (rawLine in content.replace("\r", "").lineSequence()) {
        val prefix = LINE_TIMESTAMP_PREFIX_REGEX.find(rawLine)
        if (prefix != null) {
            val body = rawLine.substring(prefix.range.last + 1).trim()
            if (body.isNotEmpty()) {
                hasTimedLyricLine = true
                val remaining = WORD_TIMESTAMP_REGEX.replace(body, "")
                if (
                    !WORD_TIMESTAMP_START_REGEX.containsMatchIn(body) ||
                    ANY_WORD_MARKER_REGEX.containsMatchIn(remaining) ||
                    remaining.isBlank()
                ) {
                    return false
                }
            }
        }
    }
    return hasTimedLyricLine
}

private val LINE_TIMESTAMP_PREFIX_REGEX =
    Regex("^\\s*(?:\\[[0-9]{1,3}:[0-5][0-9](?:[.:][0-9]{1,3})?])+(?:\\s*)")
private val WORD_TIMESTAMP_REGEX = Regex("<[0-9]{1,3}:[0-5][0-9](?:[.:][0-9]{1,3})?>")
private val WORD_TIMESTAMP_START_REGEX = Regex("^\\s*<[0-9]{1,3}:[0-5][0-9](?:[.:][0-9]{1,3})?>")
private val ANY_WORD_MARKER_REGEX = Regex("<[^>]*(?:>|$)")
