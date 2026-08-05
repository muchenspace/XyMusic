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

@Singleton
@UnstableApi
class AutomaticQualityTransferListener
@Inject
constructor(
    private val qualityController: AutomaticPlaybackQualityPolicy,
) : TransferListener {
    private val lock = Any()
    private val transfers = IdentityHashMap<DataSource, TransferWindow>()

    override fun onTransferInitializing(source: DataSource, dataSpec: DataSpec, isNetwork: Boolean) = Unit

    override fun onTransferStart(source: DataSource, dataSpec: DataSpec, isNetwork: Boolean) {
        if (!isNetwork) return
        synchronized(lock) {
            transfers[source] = TransferWindow(SystemClock.elapsedRealtime())
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
            val window = transfers[source] ?: return@synchronized null
            window.bytes += bytesTransferred
            window.takeSample(SystemClock.elapsedRealtime(), force = false)
        }
        sample?.let { qualityController.observeBandwidth(it.bitsPerSecond, it.durationMs) }
    }

    override fun onTransferEnd(source: DataSource, dataSpec: DataSpec, isNetwork: Boolean) {
        if (!isNetwork) return
        val sample = synchronized(lock) {
            transfers.remove(source)?.takeSample(SystemClock.elapsedRealtime(), force = true)
        }
        sample?.let { qualityController.observeBandwidth(it.bitsPerSecond, it.durationMs) }
    }

    private data class TransferWindow(var measuredAtMs: Long, var bytes: Long = 0) {
        fun takeSample(nowMs: Long, force: Boolean): BandwidthSample? {
            val durationMs = nowMs - measuredAtMs
            if ((!force && durationMs < SAMPLE_INTERVAL_MS) || durationMs < MIN_SAMPLE_DURATION_MS || bytes <= 0) {
                return null
            }
            val sample = BandwidthSample(bytes * 8_000.0 / durationMs, durationMs)
            measuredAtMs = nowMs
            bytes = 0
            return sample
        }
    }

    private data class BandwidthSample(val bitsPerSecond: Double, val durationMs: Long)

    private companion object {
        const val SAMPLE_INTERVAL_MS = 500L
        const val MIN_SAMPLE_DURATION_MS = 200L
    }
}
