package com.xymusic.app.feature.player.presentation

import com.google.common.truth.Truth.assertThat
import org.junit.Test

class PlayerLyricsFollowTest {
    @Test
    fun adjacentNaturalLyricsRetainTheOutgoingHighlight() {
        assertThat(
            outgoingLyricHighlightIndex(
                previousIndex = 4,
                currentIndex = 5,
                positionDiscontinuous = false,
            ),
        ).isEqualTo(4)
    }

    @Test
    fun seekAndNonAdjacentLyricsDoNotRetainAnOutgoingHighlight() {
        assertThat(
            outgoingLyricHighlightIndex(
                previousIndex = 4,
                currentIndex = 7,
                positionDiscontinuous = false,
            ),
        ).isNull()
        assertThat(
            outgoingLyricHighlightIndex(
                previousIndex = 4,
                currentIndex = 5,
                positionDiscontinuous = true,
            ),
        ).isNull()
    }

    @Test
    fun adjacentVisibleLyricsUseContinuousScrolling() {
        assertThat(
            lyricFollowScrollMode(
                previousLyricIndex = 4,
                lyricIndex = 5,
            ),
        ).isEqualTo(LyricFollowScrollMode.Animate)
    }

    @Test
    fun firstAndFarLyricsSnapWhileAdjacentUnlaidOutLyricsAnimate() {
        assertThat(
            lyricFollowScrollMode(
                previousLyricIndex = null,
                lyricIndex = 0,
            ),
        ).isEqualTo(LyricFollowScrollMode.Snap)
        assertThat(
            lyricFollowScrollMode(
                previousLyricIndex = 2,
                lyricIndex = 8,
            ),
        ).isEqualTo(LyricFollowScrollMode.Snap)
        assertThat(
            lyricFollowScrollMode(
                previousLyricIndex = 4,
                lyricIndex = 5,
            ),
        ).isEqualTo(LyricFollowScrollMode.Animate)
    }
}
