package com.xymusic.app.feature.player.data.controller

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.player.adapter.media3.PlaybackMediaMetadata
import com.xymusic.app.feature.player.adapter.media3.PlaybackSessionCommands
import com.xymusic.app.feature.player.domain.PlayerEvent
import com.xymusic.app.feature.player.domain.model.PlaybackState
import com.xymusic.app.feature.player.domain.model.PlayerConnectionState
import com.xymusic.app.feature.player.domain.model.PlayerFailure
import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
import com.xymusic.app.feature.player.domain.model.PlayerState
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class Media3PlayerRepositoryTest {
    @Test
    fun queueMediaItemPublishesDurationThroughStandardMediaMetadata() {
        val mediaItem =
            queueItem(
                queueItemId = "queue-1",
                trackId = "00000000-0000-0000-0000-000000000001",
            ).copy(durationMs = 180_000).toMedia3MediaItem()

        assertThat(mediaItem.mediaMetadata.durationMs).isEqualTo(180_000L)
        assertThat(mediaItem.mediaMetadata.extras?.getLong(PlaybackMediaMetadata.EXTRA_DURATION_MS))
            .isEqualTo(180_000L)
    }

    @Test
    fun controllerDisconnectionClearsStalePlaybackState() {
        val disconnected = disconnectedPlayerState()

        assertThat(disconnected.connectionState).isEqualTo(PlayerConnectionState.DISCONNECTED)
        assertThat(disconnected.playbackState).isEqualTo(PlaybackState.IDLE)
        assertThat(disconnected.queue).isEmpty()
        assertThat(disconnected.currentQueueItemId).isNull()
        assertThat(disconnected.isPlaying).isFalse()
        assertThat(disconnected.positionMs).isEqualTo(0)
        assertThat(disconnected.bufferedPositionMs).isEqualTo(0)
        assertThat(disconnected.durationMs).isEqualTo(0)
        assertThat(disconnected.sleepTimerRemainingMs).isNull()
        assertThat(disconnected.failure).isEqualTo(PlayerFailure.ConnectionUnavailable)
    }

    @Test
    fun positionSamplingRunsOnlyForActiveMediaPlayback() {
        assertThat(
            shouldSamplePlaybackPosition(
                isPlaying = true,
                hasCurrentMediaItem = true,
            ),
        ).isTrue()
        assertThat(
            shouldSamplePlaybackPosition(
                isPlaying = false,
                hasCurrentMediaItem = true,
            ),
        ).isFalse()
        assertThat(
            shouldSamplePlaybackPosition(
                isPlaying = true,
                hasCurrentMediaItem = false,
            ),
        ).isFalse()
        assertThat(
            shouldSamplePlaybackPosition(
                isPlaying = false,
                hasCurrentMediaItem = false,
            ),
        ).isFalse()
    }

    @Test
    fun progressSampleRefreshesTheAnchorEvenWhenProgressValuesAreUnchanged() {
        val previous =
            PlayerState(
                currentQueueItemId = "queue-1",
                isPlaying = true,
                positionMs = 1_000,
                positionAnchorElapsedRealtimeMs = 10_000,
                positionDiscontinuitySequence = 3,
                bufferedPositionMs = 2_000,
                durationMs = 10_000,
            )

        val sampled =
            playerStateWithProgressSample(
                previous = previous,
                positionMs = 1_000,
                bufferedPositionMs = 2_000,
                durationMs = 10_000,
                sampledAtElapsedRealtimeMs = 11_000,
            )

        assertThat(sampled).isNotEqualTo(previous)
        assertThat(sampled.positionAnchorElapsedRealtimeMs).isEqualTo(11_000)
        assertThat(sampled.positionDiscontinuitySequence).isEqualTo(3)
    }

    @Test
    fun coldStartSeekPublishesTheTargetBeforeTheControllerTimelineIsRestored() {
        val previous =
            PlayerState(
                connectionState = PlayerConnectionState.CONNECTED,
                currentQueueItemId = "queue-1",
                positionMs = 1_000,
                positionDiscontinuitySequence = 3,
                bufferedPositionMs = 2_000,
                durationMs = 10_000,
            )
        val emptyControllerState =
            PlayerState(
                connectionState = PlayerConnectionState.CONNECTED,
                positionMs = 0,
                bufferedPositionMs = 0,
                durationMs = 0,
            )

        val published =
            playerStateWithPublishedPosition(
                previous = previous,
                controllerState = emptyControllerState,
                publishedPositionMs = 8_000,
                sampledAtElapsedRealtimeMs = 20_000,
                positionDiscontinuity = true,
            )

        assertThat(published.currentQueueItemId).isEqualTo("queue-1")
        assertThat(published.positionMs).isEqualTo(8_000)
        assertThat(published.positionAnchorElapsedRealtimeMs).isEqualTo(20_000)
        assertThat(published.positionDiscontinuitySequence).isEqualTo(4)
        assertThat(published.bufferedPositionMs).isEqualTo(2_000)
    }

    @Test
    fun trackTransitionClearsStalePositionAndIncrementsDiscontinuitySequence() {
        val previous =
            PlayerState(
                connectionState = PlayerConnectionState.CONNECTED,
                currentQueueItemId = "queue-1",
                positionMs = 210_000,
                positionDiscontinuitySequence = 3,
                bufferedPositionMs = 210_000,
                durationMs = 210_000,
            )
        val nextControllerState =
            PlayerState(
                connectionState = PlayerConnectionState.CONNECTED,
                currentQueueItemId = "queue-2",
                positionMs = 210_000,
                bufferedPositionMs = 210_000,
                durationMs = 180_000,
            )

        val updated =
            playerStateWithPublishedPosition(
                previous = previous,
                controllerState = nextControllerState,
                publishedPositionMs = null,
                sampledAtElapsedRealtimeMs = 30_000,
                positionDiscontinuity = false,
            )

        assertThat(updated.currentQueueItemId).isEqualTo("queue-2")
        assertThat(updated.positionMs).isEqualTo(0L)
        assertThat(updated.bufferedPositionMs).isEqualTo(0L)
        assertThat(updated.positionDiscontinuitySequence).isEqualTo(4)
        assertThat(updated.durationMs).isEqualTo(180_000)
    }

    @Test
    fun positionDiscontinuitySequenceChangesOnlyForTimelineDiscontinuities() {
        val previous =
            PlayerState(
                currentQueueItemId = "queue-1",
                isPlaying = true,
                positionDiscontinuitySequence = 3,
            )

        assertThat(
            nextPositionDiscontinuitySequence(
                previous = previous,
                currentQueueItemId = "queue-1",
                explicitDiscontinuity = false,
            ),
        ).isEqualTo(3)
        assertThat(
            nextPositionDiscontinuitySequence(
                previous = previous,
                currentQueueItemId = "queue-1",
                explicitDiscontinuity = true,
            ),
        ).isEqualTo(4)
        assertThat(
            nextPositionDiscontinuitySequence(
                previous = previous,
                currentQueueItemId = "queue-2",
                explicitDiscontinuity = false,
            ),
        ).isEqualTo(4)
        assertThat(
            nextPositionDiscontinuitySequence(
                previous = previous,
                currentQueueItemId = "queue-1",
                explicitDiscontinuity = false,
            ),
        ).isEqualTo(3)
    }

    @Test
    fun positionDiscontinuityIsNotMarkedForNoOpOrFailedCommands() {
        assertThat(
            shouldMarkPositionDiscontinuity(
                commandSucceeded = true,
                didChangePosition = false,
            ),
        ).isFalse()
        assertThat(
            shouldMarkPositionDiscontinuity(
                commandSucceeded = false,
                didChangePosition = true,
            ),
        ).isFalse()
        assertThat(
            shouldMarkPositionDiscontinuity(
                commandSucceeded = true,
                didChangePosition = true,
            ),
        ).isTrue()
    }

    @Test
    fun coldStartSeekWaitsForTheFirstRestoredMediaItem() {
        val pendingSeek = PendingRestoredMediaItemSeek()

        pendingSeek.defer(expectedQueueItemId = null, positionMs = 12_345)

        assertThat(pendingSeek.takeForRestoredItem(queueItemId = null)).isNull()
        assertThat(pendingSeek.takeForRestoredItem(queueItemId = "queue-1")).isEqualTo(12_345)
        assertThat(pendingSeek.takeForRestoredItem(queueItemId = "queue-1")).isNull()
    }

    @Test
    fun consecutiveColdStartSeeksOnlyApplyTheLatestPosition() {
        val pendingSeek = PendingRestoredMediaItemSeek()

        pendingSeek.defer(expectedQueueItemId = null, positionMs = 1_000)
        pendingSeek.defer(expectedQueueItemId = null, positionMs = 9_000)

        assertThat(pendingSeek.takeForRestoredItem(queueItemId = "queue-1")).isEqualTo(9_000)
    }

    @Test
    fun coldStartSeekCannotLeakOntoADifferentRestoredQueueItem() {
        val pendingSeek = PendingRestoredMediaItemSeek()

        pendingSeek.defer(expectedQueueItemId = "queue-1", positionMs = 8_000)

        assertThat(pendingSeek.takeForRestoredItem(queueItemId = "queue-2")).isNull()
        assertThat(pendingSeek.takeForRestoredItem(queueItemId = "queue-1")).isNull()
    }

    @Test
    fun explicitQueueActionCancelsAColdStartSeek() {
        val pendingSeek = PendingRestoredMediaItemSeek()

        pendingSeek.defer(expectedQueueItemId = null, positionMs = 8_000)
        pendingSeek.cancel()

        assertThat(pendingSeek.takeForRestoredItem(queueItemId = "queue-1")).isNull()
    }

    @Test
    fun sleepTimerRemainingTimeIsDerivedFromAuthoritativeDeadline() {
        assertThat(remainingSleepTimerMs(70_000L, 25_000L)).isEqualTo(45_000L)
        assertThat(remainingSleepTimerMs(70_000L, 70_000L)).isNull()
        assertThat(remainingSleepTimerMs(70_000L, 80_000L)).isNull()
        assertThat(remainingSleepTimerMs(null, 25_000L)).isNull()
    }

    @Test
    fun queueValidationRejectsDuplicateIdsInvalidStartAndNegativePosition() {
        val first = queueItem("first", "00000000-0000-0000-0000-000000000001")
        val second = queueItem("second", "00000000-0000-0000-0000-000000000002")

        assertThat(isValidPlayerQueue(listOf(first, second), "second", 10)).isTrue()
        assertThat(isValidPlayerQueue(listOf(first, first), "first", 0)).isFalse()
        assertThat(isValidPlayerQueue(listOf(first), "missing", 0)).isFalse()
        assertThat(isValidPlayerQueue(listOf(first), "first", -1)).isFalse()
    }

    @Test
    fun codecFallbackSessionCommandMapsToOneShotPlayerEvent() {
        assertThat(
            playerEventForCustomAction(
                PlaybackSessionCommands.ACTION_CODEC_FALLBACK_APPLIED,
            ),
        ).isEqualTo(PlayerEvent.CompatibleCodecFallbackApplied)
        assertThat(playerEventForCustomAction("unsupported")).isNull()
    }

    private fun queueItem(queueItemId: String, trackId: String) = PlayerQueueItem(
        queueItemId = queueItemId,
        trackId = trackId,
        title = queueItemId,
        artistNames = emptyList(),
        albumTitle = null,
        artworkUrl = null,
        artworkCacheKey = null,
        durationMs = 1,
    )
}
