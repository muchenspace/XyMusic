package com.xymusic.app.feature.player.data.quality

import android.os.SystemClock
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.DataSpec
import androidx.media3.datasource.TransferListener
import com.xymusic.app.feature.player.domain.AutomaticPlaybackQualityPolicy
import java.util.IdentityHashMap
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Samples network throughput from byte transfers.
 *
 * A single HLS segment on a fast network completes within a few milliseconds,
 * far below the interval that yields a stable per-request estimate. Short
 * transfers are therefore merged into one session window measured in
 * wall-clock span (including the gaps the player leaves between requests):
 * the merged sample is emitted as soon as it holds enough bytes over enough
 * elapsed time to be meaningful. Long transfers keep emitting periodic
 * samples every [WINDOW_SAMPLE_INTERVAL_MS] and a final sample when they
 * close.
 *
 * All emissions are capped at [MAX_MEANINGFUL_BPS]: the quality ladder's top
 * tier needs well under 10% of that value, so anything above the cap is
 * equivalent for selection purposes, while raw LAN-rate readings (100Mbps+)
 * would otherwise be rejected by the estimator's sanity filter.
 */
@Singleton
@UnstableApi
class AutomaticQualityTransferListener
@Inject
constructor(
    private val qualityController: AutomaticPlaybackQualityPolicy,
) : TransferListener {
    private var clock: () -> Long = SystemClock::elapsedRealtime

    internal constructor(
        qualityController: AutomaticPlaybackQualityPolicy,
        elapsedRealtime: () -> Long,
    ) : this(qualityController) {
        clock = elapsedRealtime
    }

    private val lock = Any()
    private val transfers = IdentityHashMap<DataSource, TransferWindow>()
    private var aggregateBytes = 0L
    private var aggregateDurationMs = 0L
    private var aggregateStartedAtMs = 0L
    private var lastAggregateActivityMs = 0L

    override fun onTransferInitializing(source: DataSource, dataSpec: DataSpec, isNetwork: Boolean) = Unit

    override fun onTransferStart(source: DataSource, dataSpec: DataSpec, isNetwork: Boolean) {
        if (!isNetwork) return
        synchronized(lock) {
            transfers[source] = TransferWindow(clock())
        }
    }

    override fun onBytesTransferred(
        source: DataSource,
        dataSpec: DataSpec,
        isNetwork: Boolean,
        bytesTransferred: Int,
    ) {
        if (!isNetwork || bytesTransferred <= 0) return
        val sample = synchronized(lock) {
            transfers[source]
                ?.apply { bytes += bytesTransferred }
                ?.takeSample(clock())
        }
        sample?.let { qualityController.observeBandwidth(it.bitsPerSecond, it.durationMs) }
    }

    override fun onTransferEnd(source: DataSource, dataSpec: DataSpec, isNetwork: Boolean) {
        if (!isNetwork) return
        val sample = synchronized(lock) {
            val now = clock()
            val window = transfers.remove(source) ?: return@synchronized null
            val endSample = window.takeEndSample(now) ?: return@synchronized null
            if (endSample.durationMs >= DIRECT_SAMPLE_MIN_MS) {
                BandwidthSample(
                    cappedBandwidth(endSample.bytes * 8_000.0 / endSample.durationMs),
                    endSample.durationMs,
                )
            } else {
                mergeShortTransfer(now, endSample)
            }
        }
        sample?.let { qualityController.observeBandwidth(it.bitsPerSecond, it.durationMs) }
    }

    /**
     * Merges a short transfer into the session window and returns a sample once
     * the window has accumulated enough bytes over enough wall-clock time to be
     * meaningful. The window span starts at the first transfer of the burst, so
     * request scheduling gaps and RTT are counted as transfer time.
     */
    private fun mergeShortTransfer(nowMs: Long, transfer: BytesSample): BandwidthSample? {
        if (aggregateBytes == 0L || nowMs - lastAggregateActivityMs > SESSION_IDLE_RESET_MS) {
            // A long idle gap means the pending window belongs to an earlier
            // burst (previous track, paused playback, or a network switch).
            // Start fresh so stale bytes cannot skew the new estimate.
            aggregateBytes = transfer.bytes
            aggregateDurationMs = transfer.durationMs
            aggregateStartedAtMs = nowMs - transfer.durationMs
        } else {
            aggregateBytes += transfer.bytes
            aggregateDurationMs = (nowMs - aggregateStartedAtMs).coerceAtLeast(transfer.durationMs)
        }
        lastAggregateActivityMs = nowMs
        if (aggregateBytes < SESSION_MIN_EMIT_BYTES || aggregateDurationMs < SESSION_MIN_EMIT_DURATION_MS) {
            return null
        }
        val bytes = aggregateBytes
        val durationMs = aggregateDurationMs
        aggregateBytes = 0
        aggregateDurationMs = 0
        aggregateStartedAtMs = 0
        return BandwidthSample(cappedBandwidth(bytes * 8_000.0 / durationMs), durationMs)
    }

    private class TransferWindow(
        var startedAtMs: Long,
        var bytes: Long = 0,
    ) {
        fun takeSample(nowMs: Long): BandwidthSample? {
            val durationMs = nowMs - startedAtMs
            if (durationMs < WINDOW_SAMPLE_INTERVAL_MS || bytes <= 0) return null
            val sample = BandwidthSample(cappedBandwidth(bytes * 8_000.0 / durationMs), durationMs)
            startedAtMs = nowMs
            bytes = 0
            return sample
        }

        fun takeEndSample(nowMs: Long): BytesSample? {
            val durationMs = nowMs - startedAtMs
            if (durationMs <= 0 || bytes <= 0) return null
            val sample = BytesSample(bytes, durationMs)
            startedAtMs = nowMs
            bytes = 0
            return sample
        }
    }

    private data class BandwidthSample(val bitsPerSecond: Double, val durationMs: Long)

    private data class BytesSample(val bytes: Long, val durationMs: Long)

    private companion object {
        const val WINDOW_SAMPLE_INTERVAL_MS = 500L
        const val DIRECT_SAMPLE_MIN_MS = 200L
        const val SESSION_MIN_EMIT_BYTES = 24_000L
        const val SESSION_MIN_EMIT_DURATION_MS = 30L
        const val SESSION_IDLE_RESET_MS = 1_500L
    }
}

/**
 * Any LAN/fast-network reading above this is equivalent for quality selection
 * (the top tier needs 2.3Mbps after the estimator's safety factor), and
 * staying below the estimator's 100Mbps sanity filter keeps such samples
 * meaningful instead of rejected.
 */
private const val MAX_MEANINGFUL_BPS = 50_000_000.0

private fun cappedBandwidth(bitsPerSecond: Double): Double =
    if (bitsPerSecond > MAX_MEANINGFUL_BPS) MAX_MEANINGFUL_BPS else bitsPerSecond
