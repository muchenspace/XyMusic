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
    fun anchoredPositionTracksElapsedTimeSpeedAndDuration() {
        assertThat(
            anchoredPlaybackPositionMs(
                player =
                PlayerState(
                    isPlaying = true,
                    positionMs = 1_000,
                    positionAnchorElapsedRealtimeMs = 10_000,
                    durationMs = 10_000,
                    playbackSpeed = 0.5f,
                ),
                nowElapsedRealtimeMs = 12_000,
            ),
        ).isEqualTo(2_000f)
        assertThat(
            anchoredPlaybackPositionMs(
                player =
                PlayerState(
                    isPlaying = true,
                    positionMs = 1_000,
                    positionAnchorElapsedRealtimeMs = 10_000,
                    durationMs = 10_000,
                    playbackSpeed = 2f,
                ),
                nowElapsedRealtimeMs = 12_000,
            ),
        ).isEqualTo(5_000f)
        assertThat(
            anchoredPlaybackPositionMs(
                player =
                PlayerState(
                    isPlaying = true,
                    positionMs = 9_500,
                    positionAnchorElapsedRealtimeMs = 10_000,
                    durationMs = 10_000,
                    playbackSpeed = 2f,
                ),
                nowElapsedRealtimeMs = 12_000,
            ),
        ).isEqualTo(10_000f)
    }

    @Test
    fun anchoredPositionDoesNotAdvanceWithoutAnAnchorOrWhilePaused() {
        assertThat(
            anchoredPlaybackPositionMs(
                player = PlayerState(isPlaying = true, positionMs = 1_000, playbackSpeed = 2f),
                nowElapsedRealtimeMs = 12_000,
            ),
        ).isEqualTo(1_000f)
        assertThat(
            anchoredPlaybackPositionMs(
                player =
                PlayerState(
                    isPlaying = false,
                    positionMs = 1_000,
                    positionAnchorElapsedRealtimeMs = 10_000,
                    playbackSpeed = 2f,
                ),
                nowElapsedRealtimeMs = 12_000,
            ),
        ).isEqualTo(1_000f)
    }

    @Test
    fun discontinuitiesSnapButOrdinarySamplesOnlyNeedCorrection() {
        val previousSample =
            PlaybackPositionClockSample(
                currentQueueItemId = "queue-1",
                discontinuitySequence = 4,
                isPlaying = true,
            )
        val ordinaryPlayer =
            PlayerState(
                currentQueueItemId = "queue-1",
                isPlaying = true,
                positionMs = 1_000,
                positionAnchorElapsedRealtimeMs = 10_000,
                positionDiscontinuitySequence = 4,
            )

        assertThat(
            shouldSnapPlaybackPosition(
                previousSample = previousSample,
                player = ordinaryPlayer,
                displayedPositionMs = 1_040f,
                nowElapsedRealtimeMs = 10_050,
            ),
        ).isFalse()
        assertThat(
            shouldSnapPlaybackPosition(
                previousSample = previousSample,
                player = ordinaryPlayer.copy(positionDiscontinuitySequence = 5),
                displayedPositionMs = 1_040f,
                nowElapsedRealtimeMs = 10_050,
            ),
        ).isTrue()
        assertThat(
            shouldSnapPlaybackPosition(
                previousSample = previousSample,
                player = ordinaryPlayer.copy(currentQueueItemId = "queue-2"),
                displayedPositionMs = 1_040f,
                nowElapsedRealtimeMs = 10_050,
            ),
        ).isTrue()
    }

    @Test
    fun ordinaryCorrectionsNeverMoveTheRenderedPositionBackward() {
        assertThat(monotonicPlaybackPosition(previousPositionMs = 1_040f, candidatePositionMs = 1_020f))
            .isEqualTo(1_040f)
        assertThat(monotonicPlaybackPosition(previousPositionMs = 1_040f, candidatePositionMs = 1_060f))
            .isEqualTo(1_060f)
    }
}
