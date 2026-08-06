package com.xymusic.app.feature.player.presentation

import com.google.common.truth.Truth.assertThat
import org.junit.Test

class PlayerLyricsFollowTest {
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

    @Test
    fun denseTimestampJumpUsesAnimationWhileSparseTimestampJumpSnaps() {
        assertThat(
            lyricFollowScrollMode(
                previousLyricIndex = 2,
                lyricIndex = 5,
                previousLyricTimeMs = 1_000,
                lyricTimeMs = 1_450,
            ),
        ).isEqualTo(LyricFollowScrollMode.Animate)
        assertThat(
            lyricFollowScrollMode(
                previousLyricIndex = 2,
                lyricIndex = 5,
                previousLyricTimeMs = 1_000,
                lyricTimeMs = 1_451,
            ),
        ).isEqualTo(LyricFollowScrollMode.Snap)
        assertThat(
            lyricFollowScrollMode(
                previousLyricIndex = 2,
                lyricIndex = 5,
                previousLyricTimeMs = 1_450,
                lyricTimeMs = 1_000,
            ),
        ).isEqualTo(LyricFollowScrollMode.Snap)
    }

    @Test
    fun interruptedTransitionKeepsTheCurrentVisualPosition() {
        assertThat(
            lyricTransitionStartPosition(
                activeLinePosition = 19.35f,
            ),
        ).isEqualTo(19.35f)
    }

    @Test
    fun seekBaselineWaitsForTheTargetInsteadOfSettlingOnAnIntermediateLine() {
        assertThat(
            lyricSeekBaselineIndex(sourceIndex = 4, targetIndex = 20, currentIndex = 19),
        ).isNull()
        assertThat(
            lyricSeekBaselineIndex(sourceIndex = 4, targetIndex = 20, currentIndex = 20),
        ).isEqualTo(20)
        assertThat(
            lyricSeekBaselineIndex(sourceIndex = 4, targetIndex = 20, currentIndex = 21),
        ).isEqualTo(20)
        assertThat(
            lyricSeekBaselineIndex(sourceIndex = 20, targetIndex = 4, currentIndex = 5),
        ).isNull()
        assertThat(
            lyricSeekBaselineIndex(sourceIndex = 20, targetIndex = 4, currentIndex = 3),
        ).isEqualTo(4)
    }

    @Test
    fun missingLyricLayoutIsNotConsideredASettledBaseline() {
        assertThat(lyricLayoutDeltaHasSettled(previousDelta = null, currentDelta = null)).isFalse()
        assertThat(lyricLayoutDeltaHasSettled(previousDelta = null, currentDelta = 0f)).isFalse()
        assertThat(lyricLayoutDeltaHasSettled(previousDelta = 10f, currentDelta = null)).isFalse()
        assertThat(lyricLayoutDeltaHasSettled(previousDelta = 10f, currentDelta = 10.4f)).isTrue()
        assertThat(lyricLayoutDeltaHasSettled(previousDelta = 10f, currentDelta = 10.6f)).isFalse()
    }
}
