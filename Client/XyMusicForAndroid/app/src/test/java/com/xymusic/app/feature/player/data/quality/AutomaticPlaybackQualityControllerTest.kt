package com.xymusic.app.feature.player.data.quality

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.domain.settings.StreamingQuality
import com.xymusic.app.feature.player.domain.model.PreferredQuality
import org.junit.Test

class AutomaticPlaybackQualityControllerTest {
    @Test
    fun automaticQualityBootstrapsAtStandardAndUpgradesAfterStableTracks() {
        val controller = AutomaticPlaybackQualityController()

        assertThat(controller.resolveTrackQuality(TRACK_1, StreamingQuality.AUTO))
            .isEqualTo(PreferredQuality.STANDARD)
        controller.onTrackStarted(TRACK_1)
        controller.observeBandwidth(1_000_000.0, 500)
        controller.observeBandwidth(1_000_000.0, 500)
        controller.finishActiveTrack()

        assertThat(controller.resolveTrackQuality(TRACK_2, StreamingQuality.AUTO))
            .isEqualTo(PreferredQuality.STANDARD)
        controller.onTrackStarted(TRACK_2)
        controller.observeBandwidth(1_000_000.0, 500)
        controller.finishActiveTrack()

        assertThat(controller.resolveTrackQuality(TRACK_3, StreamingQuality.AUTO))
            .isEqualTo(PreferredQuality.HIGH)
    }

    @Test
    fun rebufferDowngradesImmediatelyAndRespectsCooldown() {
        val controller = highQualityController()
        controller.onTrackStarted(TRACK_3)

        assertThat(controller.onRebuffer(TRACK_3, 20_000)).isEqualTo(PreferredQuality.STANDARD)
        assertThat(controller.onRebuffer(TRACK_3, 25_000)).isNull()
        assertThat(controller.resolveTrackQuality(TRACK_3, StreamingQuality.AUTO))
            .isEqualTo(PreferredQuality.STANDARD)
    }

    @Test
    fun repeatedTrackReselectsQualityFromTheLatestBandwidthEstimate() {
        val controller = highQualityController()
        controller.onTrackStarted(TRACK_3)
        controller.observeBandwidth(400_000.0, 500)
        controller.observeBandwidth(400_000.0, 500)

        controller.onTrackStarted(TRACK_3, repeated = true)

        assertThat(controller.resolveTrackQuality(TRACK_3, StreamingQuality.AUTO))
            .isEqualTo(PreferredQuality.STANDARD)
    }

    @Test
    fun replayingAFinishedTrackDoesNotReuseItsPreviousSelection() {
        val controller = AutomaticPlaybackQualityController()
        controller.resolveTrackQuality(TRACK_1, StreamingQuality.AUTO)
        controller.onTrackStarted(TRACK_1)
        controller.observeBandwidth(1_000_000.0, 500)
        controller.observeBandwidth(1_000_000.0, 500)
        controller.finishActiveTrack()
        controller.resolveTrackQuality(TRACK_2, StreamingQuality.AUTO)
        controller.onTrackStarted(TRACK_2)
        controller.observeBandwidth(1_000_000.0, 500)
        controller.finishActiveTrack()

        assertThat(controller.resolveTrackQuality(TRACK_1, StreamingQuality.AUTO))
            .isEqualTo(PreferredQuality.HIGH)
    }

    @Test
    fun fixedPreferenceDisablesAutomaticRebufferingForTheActiveTrack() {
        val controller = AutomaticPlaybackQualityController()
        controller.resolveTrackQuality(TRACK_1, StreamingQuality.AUTO)
        controller.onTrackStarted(TRACK_1)
        controller.updateStreamingPreference(StreamingQuality.LOSSLESS)

        assertThat(controller.onRebuffer(TRACK_1, 20_000)).isNull()
        assertThat(controller.resolveTrackQuality(TRACK_2, StreamingQuality.LOSSLESS))
            .isEqualTo(PreferredQuality.LOSSLESS)
    }

    private fun highQualityController(): AutomaticPlaybackQualityController =
        AutomaticPlaybackQualityController().apply {
            resolveTrackQuality(TRACK_1, StreamingQuality.AUTO)
            onTrackStarted(TRACK_1)
            observeBandwidth(1_000_000.0, 500)
            observeBandwidth(1_000_000.0, 500)
            finishActiveTrack()
            resolveTrackQuality(TRACK_2, StreamingQuality.AUTO)
            onTrackStarted(TRACK_2)
            observeBandwidth(1_000_000.0, 500)
            finishActiveTrack()
            resolveTrackQuality(TRACK_3, StreamingQuality.AUTO)
        }

    private companion object {
        const val TRACK_1 = "11111111-1111-1111-1111-111111111111"
        const val TRACK_2 = "22222222-2222-2222-2222-222222222222"
        const val TRACK_3 = "33333333-3333-3333-3333-333333333333"
    }
}
