package com.xymusic.app.feature.player.data.controller

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.player.domain.PlayerEvent
import com.xymusic.app.feature.player.domain.model.PlaybackState
import com.xymusic.app.feature.player.domain.model.PlayerConnectionState
import com.xymusic.app.feature.player.domain.model.PlayerFailure
import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
import com.xymusic.app.feature.player.service.PlaybackSessionCommands
import org.junit.Test

class Media3PlayerRepositoryTest {
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
