package com.xymusic.app.feature.player.presentation

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.model.media.LyricsFormat
import com.xymusic.app.core.model.media.LyricsTiming
import org.junit.Test

/**
 * Regression coverage for the server-side LRC timing contract.
 *
 * These cases intentionally exercise the document boundary rather than the
 * drawing implementation. Keeping them here prevents a parser change from
 * silently dropping lines or producing backwards word intervals.
 */
class PlayerLyricsParserContractTest {
    @Test
    fun wordTimedLyricsRejectDecreasingWordTimestamps() {
        val failure = runCatching {
            parsePlayerLyrics(
                content = "[00:10]<00:11>late<00:10>early",
                format = LyricsFormat.LRC,
                timing = LyricsTiming.WORD,
                language = null,
            )
        }.exceptionOrNull()

        assertThat(failure).isInstanceOf(IllegalArgumentException::class.java)
    }

    @Test
    fun wordTimedLyricsRejectUntimedOrdinaryLinesInsteadOfDroppingThem() {
        val failure = runCatching {
            parsePlayerLyrics(
                content = "[00:01]<00:01>timed\nordinary line",
                format = LyricsFormat.LRC,
                timing = LyricsTiming.WORD,
                language = null,
            )
        }.exceptionOrNull()

        assertThat(failure).isInstanceOf(IllegalArgumentException::class.java)
    }

    @Test
    fun wordTimedLyricsRejectInvalidOrNonMetadataUntimedLines() {
        val invalidDocuments = listOf(
            "[00:01]<00:01>timed\n[ar:Artist]ordinary lyric",
            "[00:01]<00:01>timed\n[Verse]ordinary lyric",
            "[00:01]<00:01>timed\n[00:60]ordinary lyric",
            "[00:01]<00:01>timed\ntext [00:02]embedded",
        )

        invalidDocuments.forEach { content ->
            val failure = runCatching {
                parsePlayerLyrics(
                    content = content,
                    format = LyricsFormat.LRC,
                    timing = LyricsTiming.WORD,
                    language = null,
                )
            }.exceptionOrNull()

            assertThat(failure).isInstanceOf(IllegalArgumentException::class.java)
        }
    }

    @Test
    fun wordTimedLyricsAcceptEqualAndEquivalentFractionTimestamps() {
        val parsed = parsePlayerLyrics(
            content = "[00:10]<00:10.1>first<00:10:100>second<00:10:100>third",
            format = LyricsFormat.LRC,
            timing = LyricsTiming.WORD,
            language = null,
        )

        assertThat(parsed.lines.single().words.map { word -> word.timeMs })
            .containsExactly(10_100L, 10_100L, 10_100L)
            .inOrder()
        assertThat(parsed.lines.single().words.map { word -> word.endTimeMs })
            .containsExactly(10_100L, 10_100L, 10_550L)
            .inOrder()
    }

    @Test
    fun wordTimedLyricsAllowWordClockToRestartOnEachLine() {
        val parsed = parsePlayerLyrics(
            content =
            "[00:10]<00:10>first<00:11>later\r\n" +
                "[00:02]<00:02>second<00:03>later",
            format = LyricsFormat.LRC,
            timing = LyricsTiming.WORD,
            language = null,
        )

        assertThat(parsed.lines.map(PlayerLyricLineUi::timeMs))
            .containsExactly(2_000L, 10_000L)
            .inOrder()
        assertThat(parsed.lines.map { line -> line.words.map(PlayerLyricWordUi::timeMs) })
            .containsExactly(
                listOf(2_000L, 3_000L),
                listOf(10_000L, 11_000L),
            ).inOrder()
    }

    @Test
    fun metadataOnlyLinesDoNotBecomeLyrics() {
        val parsed = parsePlayerLyrics(
            content =
            "[ar:Artist]\r\n" +
                "[ti:Title]\r\n" +
                "[00:01]<00:01>line",
            format = LyricsFormat.LRC,
            timing = LyricsTiming.WORD,
            language = null,
        )

        assertThat(parsed.lines.map(PlayerLyricLineUi::text)).containsExactly("line")
    }

    @Test
    fun bomAndMixedLineEndingsAreNormalizedWithoutJoiningLyrics() {
        val parsed = parsePlayerLyrics(
            content = "\uFEFF[00:01]first\r[00:02]second\r\n[00:03]third\n[00:04]fourth",
            format = LyricsFormat.LRC,
            timing = LyricsTiming.LINE,
            language = null,
        )

        assertThat(parsed.lines.map(PlayerLyricLineUi::text))
            .containsExactly("first", "second", "third", "fourth")
            .inOrder()
    }

    @Test
    fun lineTimestampEmbeddedInTextIsNotInterpretedAsASecondLine() {
        val parsed = parsePlayerLyrics(
            content = "[00:01]first\ntext [00:02]not a prefix",
            format = LyricsFormat.LRC,
            timing = LyricsTiming.LINE,
            language = null,
        )

        assertThat(parsed.lines.map(PlayerLyricLineUi::timeMs)).containsExactly(1_000L)
        assertThat(parsed.lines.single().text).isEqualTo("first")
    }

    @Test
    fun synchronizedLineIndexUsesSortedTimestampBoundaries() {
        val parsed = parsePlayerLyrics(
            content = "[00:02]second\n[00:01]first",
            format = LyricsFormat.LRC,
            timing = LyricsTiming.LINE,
            language = null,
        )

        assertThat(parsed.currentLineIndex(999L)).isEqualTo(-1)
        assertThat(parsed.currentLineIndex(1_000L)).isEqualTo(0)
        assertThat(parsed.currentLineIndex(1_999L)).isEqualTo(0)
        assertThat(parsed.currentLineIndex(2_000L)).isEqualTo(1)
    }

    @Test
    fun duplicateLineTimestampsPreserveSourceOrderAndChooseTheLastLineAtTheBoundary() {
        val parsed = parsePlayerLyrics(
            content = "[00:01]first\n[00:01]second\n[00:02]third",
            format = LyricsFormat.LRC,
            timing = LyricsTiming.LINE,
            language = null,
        )

        assertThat(parsed.lines.map(PlayerLyricLineUi::text))
            .containsExactly("first", "second", "third")
            .inOrder()
        assertThat(parsed.currentLineIndex(1_000L)).isEqualTo(1)
    }

    @Test
    fun lrcOffsetMovesLineAndWordTimingsAndClampsNegativeResults() {
        val parsed = parsePlayerLyrics(
            content = "[offset:-1500]\n[00:01]<00:01>A<00:02>B<00:03>",
            format = LyricsFormat.LRC,
            timing = LyricsTiming.WORD,
            language = null,
        )

        assertThat(parsed.lines.single().timeMs).isEqualTo(0L)
        assertThat(parsed.lines.single().words.map(PlayerLyricWordUi::timeMs))
            .containsExactly(0L, 500L)
            .inOrder()
        assertThat(parsed.lines.single().words.map(PlayerLyricWordUi::endTimeMs))
            .containsExactly(500L, 1_500L)
            .inOrder()
    }

    @Test
    fun repeatedWordTimedLineRebasesWordIntervalsForEveryLineTimestamp() {
        val parsed = parsePlayerLyrics(
            content = "[00:01][00:10]<00:01>A<00:02>B<00:03>",
            format = LyricsFormat.LRC,
            timing = LyricsTiming.WORD,
            language = null,
        )

        assertThat(parsed.lines.map(PlayerLyricLineUi::timeMs))
            .containsExactly(1_000L, 10_000L)
            .inOrder()
        assertThat(parsed.lines.map { line -> line.words.map(PlayerLyricWordUi::timeMs) })
            .containsExactly(listOf(1_000L, 2_000L), listOf(10_000L, 11_000L))
            .inOrder()
        assertThat(parsed.lines.map { line -> line.words.map(PlayerLyricWordUi::endTimeMs) })
            .containsExactly(listOf(2_000L, 3_000L), listOf(11_000L, 12_000L))
            .inOrder()
    }

    @Test
    fun inferredFinalWordEndDoesNotCrossAnEqualOrEarlierNextLine() {
        val equalBoundary = parsePlayerLyrics(
            content = "[00:01]<00:01>A\n[00:01]<00:01>B",
            format = LyricsFormat.LRC,
            timing = LyricsTiming.WORD,
            language = null,
        )
        val earlierBoundary = parsePlayerLyrics(
            content = "[00:01]<00:02>A\n[00:01.50]<00:01.50>B",
            format = LyricsFormat.LRC,
            timing = LyricsTiming.WORD,
            language = null,
        )

        assertThat(equalBoundary.lines.first().words.single().endTimeMs).isEqualTo(1_000L)
        assertThat(earlierBoundary.lines.first().words.single().endTimeMs).isEqualTo(2_000L)
    }

    @Test
    fun negativeOffsetClampsRepeatedLineAndWordTimesWithoutExtendingAcrossTheDuplicate() {
        val parsed = parsePlayerLyrics(
            content = "[offset:-2000]\n[00:01][00:02]<00:01>A",
            format = LyricsFormat.LRC,
            timing = LyricsTiming.WORD,
            language = null,
        )

        assertThat(parsed.lines.map(PlayerLyricLineUi::timeMs))
            .containsExactly(0L, 0L)
            .inOrder()
        assertThat(parsed.lines.map { line -> line.words.single().timeMs })
            .containsExactly(0L, 0L)
            .inOrder()
        assertThat(parsed.lines.map { line -> line.words.single().endTimeMs })
            .containsExactly(0L, 240L)
            .inOrder()
    }

    @Test
    fun offsetIsParsedWhenItSharesAMetadataLine() {
        val parsed = parsePlayerLyrics(
            content = "[ar:Artist][offset:+500]\n[00:01]<00:01>A",
            format = LyricsFormat.LRC,
            timing = LyricsTiming.WORD,
            language = null,
        )

        assertThat(parsed.lines.single().timeMs).isEqualTo(1_500L)
        assertThat(parsed.lines.single().words.single().timeMs).isEqualTo(1_500L)
    }

    @Test
    fun minimumLongOffsetSaturatesAtZero() {
        val parsed = parsePlayerLyrics(
            content = "[offset:-9223372036854775808]\n[00:01]<00:01>A<00:02>",
            format = LyricsFormat.LRC,
            timing = LyricsTiming.WORD,
            language = null,
        )

        assertThat(parsed.lines.single().timeMs).isEqualTo(0L)
        assertThat(parsed.lines.single().words.map(PlayerLyricWordUi::timeMs))
            .containsExactly(0L)
        assertThat(parsed.lines.single().words.single().endTimeMs).isEqualTo(0L)
    }

    @Test
    fun explicitFinalWordEndRemainsAuthoritativePastTheNextLine() {
        val parsed = parsePlayerLyrics(
            content = "[00:01]<00:01>A<00:03>\n[00:02]<00:02>B",
            format = LyricsFormat.LRC,
            timing = LyricsTiming.WORD,
            language = null,
        )

        assertThat(parsed.lines.first().words.single().endTimeMs).isEqualTo(3_000L)
    }
}
