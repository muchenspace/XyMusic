package com.xymusic.app.feature.player.presentation

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.model.media.LyricsFormat
import com.xymusic.app.core.model.media.LyricsTiming
import org.junit.Test

class PlayerLyricsParserTest {
    @Test
    fun wordLyricsUseExplicitWordTimestamps() {
        val parsed = parsePlayerLyrics(
            content = "[00:01.00]<00:01.00>Hello <00:01.50>world<00:02.00>",
            format = LyricsFormat.LRC,
            timing = LyricsTiming.WORD,
            language = "en",
        )

        assertThat(parsed.timing).isEqualTo(LyricsTiming.WORD)
        assertThat(parsed.lines.single().words)
            .containsExactly(
                PlayerLyricWordUi(1_000, "Hello ", endTimeMs = 1_500),
                PlayerLyricWordUi(1_500, "world", endTimeMs = 2_000),
            ).inOrder()
        assertThat(parsed.lines.single().text).isEqualTo("Hello world")
    }

    @Test
    fun wordLyricsRejectContentWithoutWordTimestamps() {
        val failure =
            runCatching {
                parsePlayerLyrics(
                    content = "[00:01.00]ordinary line",
                    format = LyricsFormat.LRC,
                    timing = LyricsTiming.WORD,
                    language = "en",
                )
            }.exceptionOrNull()

        assertThat(failure).isInstanceOf(IllegalArgumentException::class.java)
    }

    @Test
    fun plainLyricsRemainStatic() {
        val parsed = parsePlayerLyrics(
            content = " First line \n\n<00:01.00>Still plain ",
            format = LyricsFormat.PLAIN,
            timing = LyricsTiming.LINE,
            language = "en",
        )

        assertThat(parsed.synchronized).isFalse()
        assertThat(parsed.timing).isEqualTo(LyricsTiming.LINE)
        assertThat(parsed.language).isEqualTo("en")
        assertThat(parsed.lines)
            .containsExactly(
                PlayerLyricLineUi(null, "First line"),
                PlayerLyricLineUi(null, "<00:01.00>Still plain"),
            ).inOrder()
        assertThat(parsed.currentLineIndex(30_000)).isEqualTo(-1)
    }

    @Test
    fun plainLyricsCannotDeclareWordTiming() {
        val failure =
            runCatching {
                parsePlayerLyrics(
                    content = "plain",
                    format = LyricsFormat.PLAIN,
                    timing = LyricsTiming.WORD,
                    language = "en",
                )
            }.exceptionOrNull()

        assertThat(failure).isInstanceOf(IllegalArgumentException::class.java)
    }

    @Test
    fun lineLyricsRejectCompleteWordTimedContent() {
        val failure =
            runCatching {
                parsePlayerLyrics(
                    content = "[00:01.00]<00:01.00>first <00:01.50>line",
                    format = LyricsFormat.LRC,
                    timing = LyricsTiming.LINE,
                    language = null,
                )
            }.exceptionOrNull()

        assertThat(failure).isInstanceOf(IllegalArgumentException::class.java)
    }

    @Test
    fun mixedLineLyricsIgnoreEmbeddedWordTimes() {
        val parsed = parsePlayerLyrics(
            content = "[00:01.00]<00:01.00>first <00:01.50>line\n[00:03.00]second line",
            format = LyricsFormat.LRC,
            timing = LyricsTiming.LINE,
            language = null,
        )

        assertThat(parsed.timing).isEqualTo(LyricsTiming.LINE)
        assertThat(parsed.lines.map(PlayerLyricLineUi::text)).containsExactly("first line", "second line").inOrder()
        assertThat(parsed.lines.all { line -> line.words.isEmpty() }).isTrue()
    }

    @Test
    fun wordLyricsRejectPrefixTextAndInvalidWordTimestamp() {
        val invalidContents =
            listOf(
                "[00:01.00]prefix<00:01.00>word",
                "[00:01.00]<00:60.00>word",
            )

        invalidContents.forEach { content ->
            val failure =
                runCatching {
                    parsePlayerLyrics(content, LyricsFormat.LRC, LyricsTiming.WORD, null)
                }.exceptionOrNull()
            assertThat(failure).isInstanceOf(IllegalArgumentException::class.java)
        }
    }

    @Test
    fun wordLyricsRejectInvalidMarkerAfterAValidWordTimestamp() {
        val invalidContents = listOf(
            "[00:01.00]<00:01.00>valid<00:60.00>invalid",
            "[00:01.00]<00:01.00>valid<bad>invalid",
        )

        invalidContents.forEach { content ->
            val failure =
                runCatching {
                    parsePlayerLyrics(content, LyricsFormat.LRC, LyricsTiming.WORD, null)
                }.exceptionOrNull()

            assertThat(failure).isInstanceOf(IllegalArgumentException::class.java)
        }
    }

    @Test
    fun lrcExpandsMultipleLineTimestampsInTimeOrder() {
        val parsed = parsePlayerLyrics(
            content =
            """
                [ar:Artist]
                [00:10.50][00:20:5][Verse] Later
                [00:01] First
            """.trimIndent(),
            format = LyricsFormat.LRC,
            timing = LyricsTiming.LINE,
            language = "und",
        )

        assertThat(parsed.lines.map(PlayerLyricLineUi::timeMs))
            .containsExactly(1_000L, 10_500L, 20_500L)
            .inOrder()
        assertThat(parsed.lines.map(PlayerLyricLineUi::text))
            .containsExactly("First", "Later", "Later")
            .inOrder()
    }

    @Test
    fun malformedAndOutOfRangeTimestampsAreIgnored() {
        val parsed = parsePlayerLyrics(
            content =
            """
                [00:60.00]Invalid seconds
                [00:01.0000]Invalid fraction
                [not-time]Metadata
                [999:59.999]Valid upper bound
            """.trimIndent(),
            format = LyricsFormat.LRC,
            timing = LyricsTiming.LINE,
            language = null,
        )

        assertThat(parsed.lines).hasSize(1)
        assertThat(parsed.lines.single().timeMs).isEqualTo(59_999_999L)
        assertThat(parsed.lines.single().text).isEqualTo("Valid upper bound")
    }
}
