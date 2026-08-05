package com.xymusic.app.feature.player.domain

import com.xymusic.app.domain.settings.StreamingQuality
import com.xymusic.app.feature.player.domain.model.PreferredQuality

interface AutomaticPlaybackQualityPolicy {
    fun resolveTrackQuality(trackId: String, preference: StreamingQuality): PreferredQuality

    fun lockConcreteQuality(trackId: String, quality: PreferredQuality): PreferredQuality

    fun recordSelectedQuality(trackId: String, quality: PreferredQuality)

    fun observeBandwidth(bitsPerSecond: Double, durationMs: Long)

    fun hasReliableEstimate(): Boolean

    fun onTrackStarted(trackId: String, repeated: Boolean = false)

    fun finishActiveTrack()

    fun onRebuffer(trackId: String, nowMs: Long): PreferredQuality?

    fun updateStreamingPreference(preference: StreamingQuality)

    fun resetNetworkEstimate()

    fun resetSession()
}
