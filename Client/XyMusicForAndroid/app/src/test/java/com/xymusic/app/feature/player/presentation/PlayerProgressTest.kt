package com.xymusic.app.feature.player.presentation

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.player.domain.model.PlayerState
import org.junit.Test

class PlayerProgressTest {
    @Test
    fun normalizedProgressHandlesBoundsAndMissingDuration() {
        assertThat(normalizedPlaybackProgress(positionMs = 50f, durationMs = 100L))
            .isEqualTo(0.5f)
        assertThat(normalizedPlaybackProgress(positionMs = -1f, durationMs = 100L))
            .isEqualTo(0f)
        assertThat(normalizedPlaybackProgress(positionMs = 101f, durationMs = 100L))
            .isEqualTo(1f)
        assertThat(normalizedPlaybackProgress(positionMs = 50f, durationMs = 0L))
            .isEqualTo(0f)
    }

    @Test
    fun projectedPositionTracksPlaybackSpeedAndDuration() {
        assertThat(
            projectedPlaybackPositionMs(
                PlayerState(
                    isPlaying = true,
                    positionMs = 1_000,
                    durationMs = 10_000,
                    playbackSpeed = 0.5f,
                ),
            ),
        ).isEqualTo(1_500)
        assertThat(
            projectedPlaybackPositionMs(
                PlayerState(
                    isPlaying = true,
                    positionMs = 1_000,
                    durationMs = 10_000,
                    playbackSpeed = 2f,
                ),
            ),
        ).isEqualTo(3_000)
        assertThat(
            projectedPlaybackPositionMs(
                PlayerState(
                    isPlaying = true,
                    positionMs = 9_500,
                    durationMs = 10_000,
                    playbackSpeed = 2f,
                ),
            ),
        ).isEqualTo(10_000)
        assertThat(
            projectedPlaybackPositionMs(
                PlayerState(
                    isPlaying = false,
                    positionMs = 1_000,
                    playbackSpeed = 2f,
                ),
            ),
        ).isEqualTo(1_000)
    }
}
