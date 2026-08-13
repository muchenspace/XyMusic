package com.xymusic.app.feature.player.presentation

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.player.domain.model.PlayerState
import org.junit.Test

class PlayerProgressTest {
    @Test
    fun structuralPlayerProjectionIgnoresPositionOnlySamples() {
        val state =
            PlayerUiState(
                player =
                PlayerState(
                    currentQueueItemId = "queue-1",
                    isPlaying = true,
                    positionMs = 1_000,
                    positionAnchorElapsedRealtimeMs = 10_000,
                    bufferedPositionMs = 2_000,
                ),
            )
        val positionOnlyUpdate =
            state.copy(
                player =
                state.player.copy(
                    positionMs = 1_250,
                    positionAnchorElapsedRealtimeMs = 10_250,
                    bufferedPositionMs = 2_500,
                ),
            )

        assertThat(positionOnlyUpdate.withoutPlaybackPosition())
            .isEqualTo(state.withoutPlaybackPosition())
        assertThat(state.player.isStructurallyEqualTo(positionOnlyUpdate.player)).isTrue()
    }

    @Test
    fun structuralPlayerProjectionIgnoresPositionDiscontinuities() {
        val state =
            PlayerState(
                currentQueueItemId = "queue-1",
                isPlaying = true,
                positionDiscontinuitySequence = 4,
                positionMs = 1_000,
            )
        val seekState = state.copy(
            positionDiscontinuitySequence = 5,
            positionMs = 24_000,
        )

        assertThat(state.isStructurallyEqualTo(seekState)).isTrue()
        assertThat(seekState.withoutPlaybackPosition().positionDiscontinuitySequence).isEqualTo(0L)
    }

    @Test
    fun structuralPlayerEqualityIncludesPlaybackStateChanges() {
        val state = PlayerState(currentQueueItemId = "queue-1", isPlaying = true)

        assertThat(state.isStructurallyEqualTo(state.copy(isPlaying = false))).isFalse()
        assertThat(state.isStructurallyEqualTo(state.copy(durationMs = 120_000))).isFalse()
        assertThat(state.isStructurallyEqualTo(state.copy(positionMs = 500))).isTrue()
    }

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
    fun interactionPositionIsQuantizedToSeconds() {
        assertThat(playbackInteractionPositionMs(12_999.9f)).isEqualTo(12_000f)
        assertThat(playbackInteractionPositionMs(13_000f)).isEqualTo(13_000f)
        assertThat(playbackInteractionPositionMs(-1f)).isEqualTo(0f)
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
    fun monotonicPositionClampsBackwardCandidates() {
        assertThat(monotonicPlaybackPosition(previousPositionMs = 1_040f, candidatePositionMs = 1_020f))
            .isEqualTo(1_040f)
        assertThat(monotonicPlaybackPosition(previousPositionMs = 1_040f, candidatePositionMs = 1_060f))
            .isEqualTo(1_060f)
    }

    @Test
    fun renderedPositionClampsOrdinaryBackwardCorrections() {
        assertThat(
            renderedPlaybackPosition(
                previousPositionMs = 1_040f,
                candidatePositionMs = 1_020f,
            ),
        ).isEqualTo(1_040f)
    }
}
