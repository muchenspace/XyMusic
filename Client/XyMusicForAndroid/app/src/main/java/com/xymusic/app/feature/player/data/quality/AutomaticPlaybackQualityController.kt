package com.xymusic.app.feature.player.data.quality

import com.xymusic.app.domain.settings.StreamingQuality
import com.xymusic.app.feature.player.domain.AutomaticPlaybackQualityPolicy
import com.xymusic.app.feature.player.domain.model.PreferredQuality
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class AutomaticPlaybackQualityController
@Inject
constructor() : AutomaticPlaybackQualityPolicy {
    private val lock = Any()
    private val trackSelections = linkedMapOf<String, TrackSelection>()
    private var fastEstimate = 0.0
    private var slowEstimate = 0.0
    private var sampleCount = 0
    private var activeTrackId: String? = null
    private var consecutiveStableTracks = 0
    private var qualityCeiling: PreferredQuality? = null
    private var lastAutomaticQuality = BOOTSTRAP_QUALITY
    private var lastDowngradeAtMs: Long? = null
    private var currentPreference = StreamingQuality.AUTO

    override fun resolveTrackQuality(trackId: String, preference: StreamingQuality): PreferredQuality {
        return synchronized(lock) {
            currentPreference = preference
            val automatic = preference == StreamingQuality.AUTO
            val existing = trackSelections[trackId]
            if (existing != null && existing.automatic == automatic) {
                if (automatic || existing.requestedQuality == preference.toConcreteQuality()) {
                    return@synchronized existing.requestedQuality
                }
            }

            val quality = if (automatic) selectAutomaticQuality() else checkNotNull(preference.toConcreteQuality())
            putSelection(
                trackId,
                TrackSelection(
                    requestedQuality = quality,
                    selectedQuality = quality,
                    automatic = automatic,
                    startSampleCount = sampleCount,
                ),
            )
            quality
        }
    }

    override fun lockConcreteQuality(trackId: String, quality: PreferredQuality): PreferredQuality =
        synchronized(lock) {
            putSelection(
                trackId,
                TrackSelection(
                    requestedQuality = quality,
                    selectedQuality = quality,
                    automatic = false,
                    startSampleCount = sampleCount,
                ),
            )
            quality
        }

    override fun updateStreamingPreference(preference: StreamingQuality) = synchronized(lock) {
        currentPreference = preference
        val activeSelection = activeTrackId?.let(trackSelections::get) ?: return@synchronized
        activeSelection.automatic = preference == StreamingQuality.AUTO
        activeSelection.startSampleCount = sampleCount
        activeSelection.stalled = false
        if (activeSelection.automatic) {
            consecutiveStableTracks = 0
            lastAutomaticQuality = activeSelection.selectedQuality
        }
    }

    override fun recordSelectedQuality(trackId: String, quality: PreferredQuality) = synchronized(lock) {
        val selection = trackSelections[trackId] ?: return@synchronized
        selection.selectedQuality = quality
        if (selection.automatic) {
            if (quality != lastAutomaticQuality) consecutiveStableTracks = 0
            lastAutomaticQuality = quality
        }
    }

    override fun observeBandwidth(bitsPerSecond: Double, durationMs: Long) = synchronized(lock) {
        if (!bitsPerSecond.isFinite() ||
            bitsPerSecond !in MIN_SAMPLE_BPS..MAX_SAMPLE_BPS ||
            durationMs < MIN_SAMPLE_DURATION_MS
        ) {
            return@synchronized
        }
        if (sampleCount == 0) {
            fastEstimate = bitsPerSecond
            slowEstimate = bitsPerSecond
        } else {
            fastEstimate = FAST_SAMPLE_WEIGHT * bitsPerSecond + (1 - FAST_SAMPLE_WEIGHT) * fastEstimate
            slowEstimate = SLOW_SAMPLE_WEIGHT * bitsPerSecond + (1 - SLOW_SAMPLE_WEIGHT) * slowEstimate
        }
        sampleCount += 1
    }

    override fun hasReliableEstimate(): Boolean = synchronized(lock) { sampleCount >= REQUIRED_SAMPLE_COUNT }

    override fun onTrackStarted(trackId: String, repeated: Boolean) = synchronized(lock) {
        if (activeTrackId == trackId && !repeated) return@synchronized
        finishActiveTrackLocked()
        activeTrackId = trackId
        trackSelections[trackId]?.apply {
            if (started) startSampleCount = sampleCount
            started = true
            stalled = false
        }
    }

    override fun finishActiveTrack() = synchronized(lock) {
        finishActiveTrackLocked()
    }

    override fun onRebuffer(trackId: String, nowMs: Long): PreferredQuality? = synchronized(lock) {
        if (currentPreference != StreamingQuality.AUTO) return@synchronized null
        val selection = trackSelections[trackId]?.takeIf(TrackSelection::automatic) ?: return@synchronized null
        val currentQuality = selection.selectedQuality
        val currentRank = currentQuality.rank()
        if (currentRank == 0 || lastDowngradeAtMs?.let { nowMs - it < DOWNGRADE_COOLDOWN_MS } == true) {
            return@synchronized null
        }

        lastDowngradeAtMs = nowMs
        selection.stalled = true
        consecutiveStableTracks = 0
        val oneStepLower = QUALITY_ORDER[currentRank - 1]
        val bandwidthTarget = qualityForBandwidth(safeBandwidth(REBUFFER_SAFETY_FACTOR))
        val target = if (bandwidthTarget.rank() < oneStepLower.rank()) bandwidthTarget else oneStepLower
        selection.requestedQuality = target
        selection.selectedQuality = target
        lastAutomaticQuality = target
        qualityCeiling = target
        target
    }

    override fun resetNetworkEstimate() = synchronized(lock) {
        fastEstimate = 0.0
        slowEstimate = 0.0
        sampleCount = 0
        consecutiveStableTracks = 0
        qualityCeiling = null
        lastAutomaticQuality = BOOTSTRAP_QUALITY
        val active = activeTrackId
        trackSelections.entries.removeAll { (trackId, selection) -> selection.automatic && trackId != active }
        Unit
    }

    override fun resetSession() = synchronized(lock) {
        trackSelections.clear()
        activeTrackId = null
        fastEstimate = 0.0
        slowEstimate = 0.0
        sampleCount = 0
        consecutiveStableTracks = 0
        qualityCeiling = null
        lastAutomaticQuality = BOOTSTRAP_QUALITY
        lastDowngradeAtMs = null
    }

    private fun selectAutomaticQuality(): PreferredQuality {
        if (sampleCount < REQUIRED_SAMPLE_COUNT) return BOOTSTRAP_QUALITY
        var candidate = qualityForBandwidth(safeBandwidth(SAFETY_FACTOR))
        qualityCeiling?.let { ceiling ->
            if (candidate.rank() > ceiling.rank()) candidate = ceiling
        }
        if (candidate.rank() > lastAutomaticQuality.rank()) {
            if (consecutiveStableTracks < STABLE_TRACKS_FOR_UPGRADE) return lastAutomaticQuality
            return QUALITY_ORDER[minOf(QUALITY_ORDER.lastIndex, lastAutomaticQuality.rank() + 1)]
        }
        return candidate
    }

    private fun finishActiveTrackLocked() {
        val trackId = activeTrackId ?: return
        val selection = trackSelections.remove(trackId)
        if (selection?.automatic == true) {
            if (selection.stalled) {
                consecutiveStableTracks = 0
            } else if (sampleCount > selection.startSampleCount) {
                consecutiveStableTracks += 1
                if (qualityCeiling != null && consecutiveStableTracks >= STABLE_TRACKS_FOR_UPGRADE) {
                    qualityCeiling =
                        QUALITY_ORDER[
                            minOf(
                                QUALITY_ORDER.lastIndex,
                                checkNotNull(qualityCeiling).rank() + 1,
                            ),
                        ]
                }
            }
        }
        activeTrackId = null
    }

    private fun safeBandwidth(factor: Double): Double =
        if (sampleCount < REQUIRED_SAMPLE_COUNT) 0.0 else minOf(fastEstimate, slowEstimate) * factor

    private fun qualityForBandwidth(bitsPerSecond: Double): PreferredQuality {
        var selected = PreferredQuality.DATA_SAVER
        QUALITY_ORDER.forEach { quality ->
            if (bitsPerSecond < REQUIRED_BANDWIDTH.getValue(quality)) return selected
            selected = quality
        }
        return selected
    }

    private fun putSelection(trackId: String, selection: TrackSelection) {
        trackSelections[trackId] = selection
        while (trackSelections.size > MAX_TRACK_SELECTIONS) {
            val removable = trackSelections.keys.firstOrNull { it != activeTrackId } ?: break
            trackSelections.remove(removable)
        }
    }

    private data class TrackSelection(
        var requestedQuality: PreferredQuality,
        var selectedQuality: PreferredQuality,
        var automatic: Boolean,
        var startSampleCount: Int = 0,
        var stalled: Boolean = false,
        var started: Boolean = false,
    )

    private companion object {
        val QUALITY_ORDER =
            listOf(
                PreferredQuality.DATA_SAVER,
                PreferredQuality.STANDARD,
                PreferredQuality.HIGH,
                PreferredQuality.LOSSLESS,
            )
        val REQUIRED_BANDWIDTH =
            mapOf(
                PreferredQuality.DATA_SAVER to 100_000.0,
                PreferredQuality.STANDARD to 200_000.0,
                PreferredQuality.HIGH to 500_000.0,
                PreferredQuality.LOSSLESS to 1_600_000.0,
            )
        val BOOTSTRAP_QUALITY = PreferredQuality.STANDARD
        const val REQUIRED_SAMPLE_COUNT = 2
        const val STABLE_TRACKS_FOR_UPGRADE = 2
        // Fast networks emit merged short-transfer samples whose window can
        // span as little as 30ms (see AutomaticQualityTransferListener).
        const val MIN_SAMPLE_DURATION_MS = 30L
        const val MIN_SAMPLE_BPS = 16_000.0
        const val MAX_SAMPLE_BPS = 100_000_000.0
        const val FAST_SAMPLE_WEIGHT = 0.5
        const val SLOW_SAMPLE_WEIGHT = 0.2
        const val SAFETY_FACTOR = 0.7
        const val REBUFFER_SAFETY_FACTOR = 0.6
        const val DOWNGRADE_COOLDOWN_MS = 15_000L
        const val MAX_TRACK_SELECTIONS = 16
    }
}

private fun StreamingQuality.toConcreteQuality(): PreferredQuality? = when (this) {
    StreamingQuality.AUTO -> null
    StreamingQuality.DATA_SAVER -> PreferredQuality.DATA_SAVER
    StreamingQuality.STANDARD -> PreferredQuality.STANDARD
    StreamingQuality.HIGH -> PreferredQuality.HIGH
    StreamingQuality.LOSSLESS -> PreferredQuality.LOSSLESS
}

private fun PreferredQuality.rank(): Int = when (this) {
    PreferredQuality.DATA_SAVER -> 0
    PreferredQuality.STANDARD -> 1
    PreferredQuality.HIGH -> 2
    PreferredQuality.LOSSLESS -> 3
}
