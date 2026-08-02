package com.xymusic.app.feature.player.presentation

import com.google.common.truth.Truth.assertThat
import org.junit.Test

class PlayerWordByWordLyricTextTest {
    private val words =
        listOf(
            PlayerLyricWordUi(1_000, "first ", endTimeMs = 2_000),
            PlayerLyricWordUi(2_000, "word", endTimeMs = 3_500),
            PlayerLyricWordUi(3_500, "!", endTimeMs = 4_000),
        )

    @Test
    fun beforeFirstWordHasNoHighlight() {
        assertThat(calculateWordTimedHighlightProgress(words, 999))
            .isEqualTo(WordTimedHighlightProgress(completedCount = 0, currentFraction = 0f))
    }

    @Test
    fun wordBetweenExplicitTimestampsUsesNextWordAsItsEnd() {
        assertThat(calculateWordTimedHighlightProgress(words, 1_500))
            .isEqualTo(WordTimedHighlightProgress(completedCount = 0, currentFraction = 0.5f))
        assertThat(calculateWordTimedHighlightProgress(words, 2_000))
            .isEqualTo(WordTimedHighlightProgress(completedCount = 1, currentFraction = 0f))
        val afterFirstWordEnd = calculateWordTimedHighlightProgress(words, 2_001)
        assertThat(afterFirstWordEnd.completedCount).isEqualTo(1)
        assertThat(afterFirstWordEnd.currentFraction).isGreaterThan(0f)
    }

    @Test
    fun finalWordUsesItsExplicitEndTime() {
        assertThat(calculateWordTimedHighlightProgress(words, 3_750))
            .isEqualTo(WordTimedHighlightProgress(completedCount = 2, currentFraction = 0.5f))
        assertThat(calculateWordTimedHighlightProgress(words, 4_000))
            .isEqualTo(WordTimedHighlightProgress(completedCount = 3, currentFraction = 0f))
        assertThat(calculateWordTimedHighlightProgress(words, 4_001))
            .isEqualTo(WordTimedHighlightProgress(completedCount = 3, currentFraction = 0f))
    }

    @Test
    fun finalWordWithoutAnExplicitEndIsFullyHighlightedAfterItStarts() {
        val unterminatedWords = listOf(PlayerLyricWordUi(1_000, "final"))

        assertThat(calculateWordTimedHighlightProgress(unterminatedWords, 999))
            .isEqualTo(WordTimedHighlightProgress(completedCount = 0, currentFraction = 0f))
        assertThat(calculateWordTimedHighlightProgress(unterminatedWords, 1_000))
            .isEqualTo(WordTimedHighlightProgress(completedCount = 0, currentFraction = 1f))
    }

    @Test
    fun zeroDurationWordDoesNotBlockFollowingWords() {
        val wordsWithZeroDurationWord =
            listOf(
                PlayerLyricWordUi(1_000, "first", endTimeMs = 2_000),
                PlayerLyricWordUi(2_000, " ", endTimeMs = 2_000),
                PlayerLyricWordUi(2_000, "second", endTimeMs = 3_000),
            )

        assertThat(calculateWordTimedHighlightProgress(wordsWithZeroDurationWord, 2_500))
            .isEqualTo(WordTimedHighlightProgress(completedCount = 2, currentFraction = 0.5f))
    }

    @Test
    fun normalizedRangesPreserveWhitespaceAndUtf16EmojiOffsets() {
        val text = "Hi 👋 世界"
        val layouts =
            normalizedTimedWordLayouts(
                text = text,
                words =
                listOf(
                    PlayerLyricWordUi(0, "Hi "),
                    PlayerLyricWordUi(500, "👋 "),
                    PlayerLyricWordUi(1_000, "世界"),
                ),
            )

        assertThat(layouts.map { it.startOffset to it.endOffset })
            .containsExactly(0 to 3, 3 to 6, 6 to 8)
            .inOrder()
        assertThat(layouts.map { layout -> text.substring(layout.startOffset, layout.endOffset) })
            .containsExactly("Hi ", "👋 ", "世界")
            .inOrder()
    }

    @Test
    fun mismatchedWordTextDisablesRangesInsteadOfHighlightingWrongCharacters() {
        assertThat(
            normalizedTimedWordLayouts(
                text = "first line",
                words = listOf(PlayerLyricWordUi(0, "first  line")),
            ),
        ).isEmpty()
    }

    @Test
    fun incompleteWordRangesDisableTheOverlayInsteadOfLeavingTextPartiallyHighlighted() {
        assertThat(
            normalizedTimedWordLayouts(
                text = "first line",
                words = listOf(PlayerLyricWordUi(0, "first")),
            ),
        ).isEmpty()
    }
}
