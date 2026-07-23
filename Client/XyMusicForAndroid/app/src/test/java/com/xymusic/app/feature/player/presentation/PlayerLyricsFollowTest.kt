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
                targetItemVisible = true,
            ),
        ).isEqualTo(LyricFollowScrollMode.Animate)
    }

    @Test
    fun firstFarAndUnlaidOutLyricsSnapToAnExactPosition() {
        assertThat(
            lyricFollowScrollMode(
                previousLyricIndex = null,
                lyricIndex = 0,
                targetItemVisible = true,
            ),
        ).isEqualTo(LyricFollowScrollMode.Snap)
        assertThat(
            lyricFollowScrollMode(
                previousLyricIndex = 2,
                lyricIndex = 8,
                targetItemVisible = true,
            ),
        ).isEqualTo(LyricFollowScrollMode.Snap)
        assertThat(
            lyricFollowScrollMode(
                previousLyricIndex = 4,
                lyricIndex = 5,
                targetItemVisible = false,
            ),
        ).isEqualTo(LyricFollowScrollMode.Snap)
    }
}
