package com.xymusic.app.feature.player.data.quality

import androidx.media3.datasource.DataSource
import androidx.media3.datasource.DataSpec
import androidx.media3.datasource.TransferListener
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.domain.settings.StreamingQuality
import com.xymusic.app.feature.player.domain.AutomaticPlaybackQualityPolicy
import com.xymusic.app.feature.player.domain.model.PreferredQuality
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class AutomaticQualityTransferListenerTest {
    @Test
    fun shortFastSegmentsAreMergedIntoAMeaningfulBandwidthSample() {
        val controller = RecordingQualityController()
        var nowMs = 10_000L
        val listener = AutomaticQualityTransferListener(controller) { nowMs }
        val source = DataSourceStub()

        // 12KB in ~30ms -> if per-request only, this would be discarded by the
        // 200ms floor. Merging two such transfers must yield one ~3.2Mbps sample.
        listener.onTransferStart(source, spec(), isNetwork = true)
        nowMs += 30
        listener.onBytesTransferred(source, spec(), isNetwork = true, bytesTransferred = 12 * 1024)
        listener.onTransferEnd(source, spec(), isNetwork = true)

        listener.onTransferStart(source, spec(), isNetwork = true)
        nowMs += 30
        listener.onBytesTransferred(source, spec(), isNetwork = true, bytesTransferred = 12 * 1024)
        listener.onTransferEnd(source, spec(), isNetwork = true)

        assertThat(controller.samples).hasSize(1)
        val sample = controller.samples.single()
        // 24KB * 8000 / 60ms = 3_276_800 bps
        assertThat(sample.bitsPerSecond).isWithin(1.0).of(3_276_800.0)
        assertThat(sample.durationMs).isEqualTo(60)
    }

    @Test
    fun mergeWindowCountsWallClockSpanIncludingRequestGaps() {
        val controller = RecordingQualityController()
        var nowMs = 10_000L
        val listener = AutomaticQualityTransferListener(controller) { nowMs }
        val source = DataSourceStub()

        // Two 12KB transfers of 30ms each, separated by a 20ms scheduling gap
        // (RTT between HLS segment requests). The merged sample must use the
        // wall-clock span (80ms), not the sum of transfer durations (60ms).
        listener.onTransferStart(source, spec(), isNetwork = true)
        nowMs += 30
        listener.onBytesTransferred(source, spec(), isNetwork = true, bytesTransferred = 12 * 1024)
        listener.onTransferEnd(source, spec(), isNetwork = true)

        nowMs += 20 // no transfer in flight: wall-clock gap between requests

        listener.onTransferStart(source, spec(), isNetwork = true)
        nowMs += 30
        listener.onBytesTransferred(source, spec(), isNetwork = true, bytesTransferred = 12 * 1024)
        listener.onTransferEnd(source, spec(), isNetwork = true)

        assertThat(controller.samples).hasSize(1)
        val sample = controller.samples.single()
        // 24KB * 8000 / 80ms = 2_457_600 bps
        assertThat(sample.bitsPerSecond).isWithin(1.0).of(2_457_600.0)
        assertThat(sample.durationMs).isEqualTo(80)
    }

    @Test
    fun lanRateSamplesAreCappedBelowTheEstimatorSanityFilter() {
        val controller = RecordingQualityController()
        var nowMs = 10_000L
        val listener = AutomaticQualityTransferListener(controller) { nowMs }
        val source = DataSourceStub()

        // 30 transfers of 12KB in 1ms each: 360KB / 30ms = 96Mbps raw. Without a
        // cap the controller's 100Mbps filter would reject it and the estimator
        // would stay starved on LAN-feed networks. The sample must be capped.
        repeat(30) {
            listener.onTransferStart(source, spec(), isNetwork = true)
            nowMs += 1
            listener.onBytesTransferred(source, spec(), isNetwork = true, bytesTransferred = 12 * 1024)
            listener.onTransferEnd(source, spec(), isNetwork = true)
        }

        assertThat(controller.samples).hasSize(1)
        val sample = controller.samples.single()
        assertThat(sample.bitsPerSecond).isEqualTo(50_000_000.0)
        assertThat(sample.durationMs).isEqualTo(30)
    }

    @Test
    fun longTransfersStillEmitPeriodicAndFinalSamples() {
        val controller = RecordingQualityController()
        var nowMs = 10_000L
        val listener = AutomaticQualityTransferListener(controller) { nowMs }
        val source = DataSourceStub()

        listener.onTransferStart(source, spec(), isNetwork = true)
        nowMs += 500
        listener.onBytesTransferred(source, spec(), isNetwork = true, bytesTransferred = 80 * 1024)
        listener.onTransferEnd(source, spec(), isNetwork = true)

        assertThat(controller.samples).hasSize(1)
        val sample = controller.samples.single()
        // 80KB * 8000 / 500ms = 1_310_720 bps
        assertThat(sample.bitsPerSecond).isWithin(1.0).of(1_310_720.0)
        assertThat(sample.durationMs).isEqualTo(500)
    }

    @Test
    fun slowTransfersAreEmittedDirectlyEvenWhenBelowTheMergeFloor() {
        val controller = RecordingQualityController()
        var nowMs = 5_000L
        val listener = AutomaticQualityTransferListener(controller) { nowMs }
        val source = DataSourceStub()

        // 16KB in 1s: slow link, sample stays direct rather than being merged.
        listener.onTransferStart(source, spec(), isNetwork = true)
        nowMs += 1_000
        listener.onBytesTransferred(source, spec(), isNetwork = true, bytesTransferred = 16 * 1024)
        listener.onTransferEnd(source, spec(), isNetwork = true)

        assertThat(controller.samples).hasSize(1)
        val sample = controller.samples.single()
        assertThat(sample.durationMs).isEqualTo(1_000)
    }

    @Test
    fun cacheReadsDoNotContributeSamples() {
        val controller = RecordingQualityController()
        var nowMs = 1_000L
        val listener = AutomaticQualityTransferListener(controller) { nowMs }
        val source = DataSourceStub()

        listener.onTransferStart(source, spec(), isNetwork = false)
        nowMs += 100
        listener.onBytesTransferred(source, spec(), isNetwork = false, bytesTransferred = 64 * 1024)
        listener.onTransferEnd(source, spec(), isNetwork = false)

        assertThat(controller.samples).isEmpty()
    }

    @Test
    fun staleAggregatedBytesAreDiscardedBeforeAMerge() {
        val controller = RecordingQualityController()
        var nowMs = 1_000L
        val listener = AutomaticQualityTransferListener(controller) { nowMs }
        val source = DataSourceStub()

        // First short burst does not reach the emit floor.
        listener.onTransferStart(source, spec(), isNetwork = true)
        nowMs += 40
        listener.onBytesTransferred(source, spec(), isNetwork = true, bytesTransferred = 8 * 1024)
        listener.onTransferEnd(source, spec(), isNetwork = true)
        assertThat(controller.samples).isEmpty()

        // An idle gap longer than the reset window must drop the stale bytes.
        nowMs += 2_000

        // Second burst reaches the floor on its own.
        listener.onTransferStart(source, spec(), isNetwork = true)
        nowMs += 50
        listener.onBytesTransferred(source, spec(), isNetwork = true, bytesTransferred = 32 * 1024)
        listener.onTransferEnd(source, spec(), isNetwork = true)

        assertThat(controller.samples).hasSize(1)
        val sample = controller.samples.single()
        // 32KB * 8000 / 50ms = 5_242_880 bps (stale 8KB not included)
        assertThat(sample.bitsPerSecond).isWithin(1.0).of(5_242_880.0)
    }

    private class RecordingQualityController : AutomaticPlaybackQualityPolicy {
        val samples = mutableListOf<BandwidthSample>()

        override fun resolveTrackQuality(trackId: String, preference: StreamingQuality): PreferredQuality =
            PreferredQuality.STANDARD

        override fun lockConcreteQuality(trackId: String, quality: PreferredQuality): PreferredQuality = quality

        override fun recordSelectedQuality(trackId: String, quality: PreferredQuality) = Unit

        override fun observeBandwidth(bitsPerSecond: Double, durationMs: Long) {
            samples += BandwidthSample(bitsPerSecond, durationMs)
        }

        override fun hasReliableEstimate(): Boolean = false

        override fun onTrackStarted(trackId: String, repeated: Boolean) = Unit

        override fun finishActiveTrack() = Unit

        override fun onRebuffer(trackId: String, nowMs: Long): PreferredQuality? = null

        override fun updateStreamingPreference(preference: StreamingQuality) = Unit

        override fun resetNetworkEstimate() = Unit

        override fun resetSession() = Unit
    }

    private data class BandwidthSample(val bitsPerSecond: Double, val durationMs: Long)

    private class DataSourceStub : DataSource {
        override fun addTransferListener(transferListener: TransferListener) = Unit

        override fun open(dataSpec: DataSpec): Long = 0

        override fun read(buffer: ByteArray, offset: Int, length: Int): Int = 0

        override fun getUri() = null

        override fun getResponseHeaders(): Map<String, List<String>> = emptyMap()

        override fun close() = Unit
    }

    private fun spec(): DataSpec = DataSpec.Builder().setUri("https://example.test/segment.m4s").build()
}
