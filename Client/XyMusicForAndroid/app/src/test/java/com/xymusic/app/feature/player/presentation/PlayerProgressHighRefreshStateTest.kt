package com.xymusic.app.feature.player.presentation

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.player.domain.model.PlayerState
import org.junit.Test

class PlayerProgressHighRefreshStateTest {
    @Test
    fun initialClockPositionProjectsAnExistingPlaybackAnchor() {
        val player =
            PlayerState(
                isPlaying = true,
                positionMs = 10_000L,
                positionAnchorElapsedRealtimeMs = 20_000L,
                durationMs = 60_000L,
            )

        assertThat(initialPlaybackPositionMs(player, nowElapsedRealtimeNanos = 20_500_000_000L))
            .isEqualTo(10_500f)
    }

    @Test
    fun anchoredPositionAdvancesMonotonicallyAtHighRefreshIntervals() {
        val player =
            PlayerState(
                currentQueueItemId = "queue-1",
                isPlaying = true,
                positionMs = 1_000L,
                positionAnchorElapsedRealtimeMs = 10_000L,
                durationMs = 10_000L,
                playbackSpeed = 1.25f,
            )

        val positions =
            listOf(10_000L, 10_008L, 10_016L).map { elapsedRealtimeMs ->
                anchoredPlaybackPositionMs(player, elapsedRealtimeMs)
            }

        assertThat(positions).containsExactly(1_000f, 1_010f, 1_020f).inOrder()
    }

    @Test
    fun preciseFrameProjectionPreservesSubMillisecondProgressAt120Hz() {
        val player =
            PlayerState(
                currentQueueItemId = "queue-1",
                isPlaying = true,
                positionMs = 1_000L,
                positionAnchorElapsedRealtimeMs = 10_000L,
                durationMs = 10_000L,
                playbackSpeed = 1.25f,
            )
        val anchorNanos = 10_000L * 1_000_000L

        val positions = listOf(
            anchoredPlaybackPositionMsPrecise(player, anchorNanos),
            anchoredPlaybackPositionMsPrecise(player, anchorNanos + 8_333_333L),
            anchoredPlaybackPositionMsPrecise(player, anchorNanos + 16_666_666L),
        )

        assertThat(positions[0]).isEqualTo(1_000f)
        assertThat(positions[1]).isWithin(0.001f).of(1_010.4166f)
        assertThat(positions[2]).isWithin(0.001f).of(1_020.8333f)
        assertThat(positions[1]).isGreaterThan(1_010f)
        assertThat(positions[2]).isGreaterThan(1_020f)
    }

    @Test
    fun routineAnchorUpdateWaitsForTheNextLoopCorrectionButSeekDoesNot() {
        val current =
            PlayerState(
                currentQueueItemId = "queue-1",
                positionDiscontinuitySequence = 7L,
                isPlaying = true,
                positionMs = 1_000L,
                positionAnchorElapsedRealtimeMs = 10_000L,
            )

        assertThat(
            hasPlaybackSampleDiscontinuity(
                current = current,
                latest = current.copy(positionMs = 2_000L, positionAnchorElapsedRealtimeMs = 11_000L),
            ),
        ).isFalse()
        assertThat(
            hasPlaybackSampleDiscontinuity(
                current = current,
                latest = current.copy(positionMs = 500L, positionDiscontinuitySequence = 8L),
            ),
        ).isTrue()
    }

    @Test
    fun aLargeBackwardCorrectionSnapsWithoutWaitingForASequenceChange() {
        val sample =
            PlaybackPositionClockSample(
                currentQueueItemId = "queue-1",
                discontinuitySequence = 7L,
                isPlaying = true,
            )
        val player =
            PlayerState(
                currentQueueItemId = "queue-1",
                positionDiscontinuitySequence = 7L,
                isPlaying = true,
                positionMs = 1_000L,
                positionAnchorElapsedRealtimeMs = 10_000L,
            )

        assertThat(
            shouldSnapPlaybackPosition(
                previousSample = sample,
                player = player,
                displayedPositionMs = 5_000f,
                nowElapsedRealtimeMs = 10_000L,
            ),
        ).isTrue()
    }

    @Test
    fun aSmallBackwardCorrectionKeepsTheRenderedClockMonotonic() {
        val sample =
            PlaybackPositionClockSample(
                currentQueueItemId = "queue-1",
                discontinuitySequence = 7L,
                isPlaying = true,
            )
        val player =
            PlayerState(
                currentQueueItemId = "queue-1",
                positionDiscontinuitySequence = 7L,
                isPlaying = true,
                positionMs = 1_000L,
                positionAnchorElapsedRealtimeMs = 10_000L,
            )

        assertThat(
            shouldSnapPlaybackPosition(
                previousSample = sample,
                player = player,
                displayedPositionMs = 1_040f,
                nowElapsedRealtimeMs = 10_000L,
            ),
        ).isFalse()
        assertThat(
            renderedPlaybackPosition(
                previousPositionMs = 1_040f,
                candidatePositionMs = 1_000f,
            ),
        ).isEqualTo(1_040f)
    }
}
