package com.xymusic.app.feature.player.data.media

import com.xymusic.app.feature.player.domain.PlaybackGrant
import com.xymusic.app.feature.player.domain.model.PreferredQuality
import java.util.LinkedHashMap
import javax.inject.Inject
import javax.inject.Singleton

data class PlaybackGrantKey(
    val ownerUserId: String,
    val sessionId: String,
    val serverGeneration: Long,
    val trackId: String,
    val preferredQuality: PreferredQuality,
    val acceptedCodecs: List<String>,
)

interface PlaybackGrantStore {
    fun get(key: PlaybackGrantKey): PlaybackGrant?

    fun put(key: PlaybackGrantKey, grant: PlaybackGrant)

    fun invalidateTrack(trackId: String)

    fun clear()
}

@Singleton
class InMemoryPlaybackGrantStore
@Inject
constructor() : PlaybackGrantStore {
    private val grants =
        object : LinkedHashMap<PlaybackGrantKey, PlaybackGrant>(
            MAX_GRANTS,
            LOAD_FACTOR,
            true,
        ) {
            override fun removeEldestEntry(eldest: MutableMap.MutableEntry<PlaybackGrantKey, PlaybackGrant>): Boolean =
                size > MAX_GRANTS
        }

    @Synchronized
    override fun get(key: PlaybackGrantKey): PlaybackGrant? = grants[key]

    @Synchronized
    override fun put(key: PlaybackGrantKey, grant: PlaybackGrant) {
        grants[key] = grant
    }

    @Synchronized
    override fun invalidateTrack(trackId: String) {
        grants.keys.removeAll { it.trackId == trackId }
    }

    @Synchronized
    override fun clear() {
        grants.clear()
    }

    private companion object {
        const val MAX_GRANTS = 512
        const val LOAD_FACTOR = 0.75f
    }
}
