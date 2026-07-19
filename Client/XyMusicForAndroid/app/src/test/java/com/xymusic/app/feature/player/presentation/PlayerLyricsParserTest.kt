package com.xymusic.app.feature.player.presentation

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.model.media.LyricsFormat
import org.junit.Test

class PlayerLyricsParserTest {
    @Test
    fun plainLyricsRemainStatic() {
        val parsed = parsePlayerLyrics(
            content = " First line \n\n<00:01.00>Still plain ",
            format = LyricsFormat.PLAIN,
            language = "en",
        )

        assertThat(parsed.synchronized).isFalse()
        assertThat(parsed.language).isEqualTo("en")
        assertThat(parsed.lines)
            .containsExactly(
                PlayerLyricLineUi(null, "First line"),
                PlayerLyricLineUi(null, "<00:01.00>Still plain"),
            ).inOrder()
        assertThat(parsed.currentLineIndex(30_000)).isEqualTo(-1)
        assertThat(estimatedWordByWordLyricProgress(parsed.lines, 30_000, 60_000)).isNull()
    }

    @Test
    fun lrcClearsLineTagsAndExpandsMultipleTimestampsInTimeOrder() {
        val parsed = parsePlayerLyrics(
            content =
            """
                [ar:Artist]
                [00:10.50][00:20:5][Verse] Later
                [00:01] First
            """.trimIndent(),
            format = LyricsFormat.LRC,
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
    fun enhancedTimestampsAreRemovedButNeverUsedForWordTiming() {
        val parsed = parsePlayerLyrics(
            content =
            "[00:00.00]<00:00.00>你<00:00.20>好\n" +
                "[00:10.00]下一行",
            format = LyricsFormat.LRC,
            language = "zh",
        )

        assertThat(parsed.lines.first().text).isEqualTo("你好")
        assertThat(parsed.lines.first().highlightEndOffsets).containsExactly(1, 2).inOrder()
        assertThat(
            estimatedWordByWordLyricProgress(parsed.lines, 1_000, 30_000)
                ?.highlightedTextEndIndex,
        ).isEqualTo(1)
        assertThat(
            estimatedWordByWordLyricProgress(parsed.lines, 5_000, 30_000)
                ?.highlightedTextEndIndex,
        ).isEqualTo(2)
    }

    @Test
    fun graphemeOffsetsKeepChineseEnglishEmojiCombiningMarksAndPunctuationIntact() {
        val parsed = parsePlayerLyrics(
            content = "[00:00]你A👍🏽é，\n[00:06]结束",
            format = LyricsFormat.LRC,
            language = null,
        )

        assertThat(parsed.lines.first().highlightEndOffsets)
            .containsExactly(1, 2, 6, 8, 9)
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
            language = null,
        )

        assertThat(parsed.lines).hasSize(1)
        assertThat(parsed.lines.single().timeMs).isEqualTo(59_999_999L)
        assertThat(parsed.lines.single().text).isEqualTo("Valid upper bound")
    }

    @Test
    fun progressUsesTheCurrentLineToNextLineIntervalAndResetsOnLineChange() {
        val parsed = parsePlayerLyrics(
            content = "[00:01]ABCD\n[00:05]Next",
            format = LyricsFormat.LRC,
            language = null,
        )

        assertThat(
            estimatedWordByWordLyricProgress(parsed.lines, 1_000, 20_000)
                ?.highlightedTextEndIndex,
        ).isEqualTo(1)
        assertThat(
            estimatedWordByWordLyricProgress(parsed.lines, 2_000, 20_000)
                ?.highlightedTextEndIndex,
        ).isEqualTo(2)
        assertThat(
            estimatedWordByWordLyricProgress(parsed.lines, 4_999, 20_000)
                ?.highlightedTextEndIndex,
        ).isEqualTo(4)
        val nextLineProgress = estimatedWordByWordLyricProgress(parsed.lines, 5_000, 20_000)
        assertThat(nextLineProgress?.lineIndex).isEqualTo(1)
        assertThat(nextLineProgress?.highlightedTextEndIndex).isEqualTo(1)
    }

    @Test
    fun finalLineUsesTrackDurationWithFallbackAndMaximum() {
        val parsed = parsePlayerLyrics(
            content = "[00:10]Final",
            format = LyricsFormat.LRC,
            language = null,
        )

        assertThat(
            estimatedWordByWordLyricProgress(parsed.lines, 10_000, 14_000)
                ?.lineEndTimeMs,
        ).isEqualTo(14_000)
        assertThat(
            estimatedWordByWordLyricProgress(parsed.lines, 10_000, 0)
                ?.lineEndTimeMs,
        ).isEqualTo(16_000)
        assertThat(
            estimatedWordByWordLyricProgress(parsed.lines, 10_000, 100_000)
                ?.lineEndTimeMs,
        ).isEqualTo(22_000)
        assertThat(estimatedWordByWordLyricProgress(parsed.lines, 22_000, 100_000)).isNull()
    }
}
