package com.xymusic.app.feature.player.presentation

import com.google.common.truth.Truth.assertThat
import org.junit.Test

class PlayerWordByWordMalformedTextTest {
    @Test
    fun normalizedRangesRejectAWordBoundaryInsideAnEmojiSurrogatePair() {
        val text = "A\uD83D\uDE00B"
        val words =
            listOf(
                PlayerLyricWordUi(0L, "A"),
                PlayerLyricWordUi(1L, "\uD83D"),
                PlayerLyricWordUi(2L, "\uDE00B"),
            )

        assertThat(normalizedTimedWordLayouts(text, words)).isEmpty()
    }

    @Test
    fun normalizedRangesRejectAWordBoundaryBeforeACombiningMark() {
        val text = "e\u0301"
        val words =
            listOf(
                PlayerLyricWordUi(0L, "e"),
                PlayerLyricWordUi(1L, "\u0301"),
            )

        assertThat(normalizedTimedWordLayouts(text, words)).isEmpty()
    }

    @Test
    fun normalizedRangesRejectAWordBoundaryInsideAZeroWidthJoinerSequence() {
        val text = "\uD83D\uDC69\u200D\uD83D\uDCBB"
        val words =
            listOf(
                PlayerLyricWordUi(0L, "\uD83D\uDC69"),
                PlayerLyricWordUi(1L, "\u200D"),
                PlayerLyricWordUi(2L, "\uD83D\uDCBB"),
            )

        assertThat(normalizedTimedWordLayouts(text, words)).isEmpty()
    }

    @Test
    fun isolatedSurrogatesDoNotCrashCharacterOffsetCalculation() {
        val result = runCatching {
            characterFragmentOffsets("\uD83D", startOffset = 0, endOffset = 1)
        }

        assertThat(result.exceptionOrNull()).isNull()
        assertThat(result.getOrThrow()).containsExactly(0 to 1)
    }

    @Test
    fun malformedCharacterRangesReturnNoFragments() {
        val text = "abc"

        assertThat(characterFragmentOffsets(text, startOffset = -1, endOffset = 2)).isEmpty()
        assertThat(characterFragmentOffsets(text, startOffset = 2, endOffset = 1)).isEmpty()
        assertThat(characterFragmentOffsets(text, startOffset = 0, endOffset = 4)).isEmpty()
    }

    @Test
    fun duplicateCompletionTimesAdvanceToTheNextWordAtTheSharedBoundary() {
        val words =
            listOf(
                PlayerLyricWordUi(1_000L, "A", endTimeMs = 2_000L),
                PlayerLyricWordUi(2_000L, "B", endTimeMs = 2_000L),
                PlayerLyricWordUi(2_000L, "C", endTimeMs = 3_000L),
            )

        assertThat(calculateWordTimedHighlightProgress(words, 2_000L))
            .isEqualTo(WordTimedHighlightProgress(completedCount = 2, currentFraction = 0f))
        assertThat(calculateWordTimedHighlightProgress(words, 2_500L))
            .isEqualTo(WordTimedHighlightProgress(completedCount = 2, currentFraction = 0.5f))
    }
}
