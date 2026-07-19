package com.xymusic.app.feature.player.presentation

import com.google.common.truth.Truth.assertThat
import org.junit.Test

class PlayerWordByWordLyricTextTest {
    @Test
    fun lineStartHasNoCompletedGraphemes() {
        val progress =
            calculateWordByWordHighlightProgress(
                playbackPositionMs = 1_000f,
                lineStartTimeMs = 1_000,
                lineEndTimeMs = 3_000,
                graphemeCount = 4,
            )

        assertThat(progress.completedCount).isEqualTo(0)
        assertThat(progress.currentFraction).isEqualTo(0f)
    }

    @Test
    fun middleOfCurrentGraphemeReturnsContinuousFraction() {
        val progress =
            calculateWordByWordHighlightProgress(
                playbackPositionMs = 2_250f,
                lineStartTimeMs = 1_000,
                lineEndTimeMs = 3_000,
                graphemeCount = 4,
            )

        assertThat(progress.completedCount).isEqualTo(2)
        assertThat(progress.currentFraction).isWithin(0.0001f).of(0.5f)
    }

    @Test
    fun lineEndCompletesEveryGrapheme() {
        val progress =
            calculateWordByWordHighlightProgress(
                playbackPositionMs = 3_000f,
                lineStartTimeMs = 1_000,
                lineEndTimeMs = 3_000,
                graphemeCount = 4,
            )

        assertThat(progress.completedCount).isEqualTo(4)
        assertThat(progress.currentFraction).isEqualTo(0f)
    }

    @Test
    fun positionsOutsideLineAreClamped() {
        val beforeLine =
            calculateWordByWordHighlightProgress(
                playbackPositionMs = 500f,
                lineStartTimeMs = 1_000,
                lineEndTimeMs = 3_000,
                graphemeCount = 4,
            )
        val afterLine =
            calculateWordByWordHighlightProgress(
                playbackPositionMs = 4_000f,
                lineStartTimeMs = 1_000,
                lineEndTimeMs = 3_000,
                graphemeCount = 4,
            )

        assertThat(beforeLine.completedCount).isEqualTo(0)
        assertThat(beforeLine.currentFraction).isEqualTo(0f)
        assertThat(afterLine.completedCount).isEqualTo(4)
        assertThat(afterLine.currentFraction).isEqualTo(0f)
    }
}
