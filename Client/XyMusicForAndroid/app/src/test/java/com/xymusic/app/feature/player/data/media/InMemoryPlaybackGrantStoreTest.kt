package com.xymusic.app.feature.player.data.media

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.player.domain.PlaybackGrant
import com.xymusic.app.feature.player.domain.model.PreferredQuality
import org.junit.Test

class InMemoryPlaybackGrantStoreTest {
    @Test
    fun leastRecentlyUsedGrantIsEvictedWhenCapacityIsExceeded() {
        val store = InMemoryPlaybackGrantStore()
        val keys = List(513, ::key)
        keys.take(512).forEach { key -> store.put(key, grant(key.trackId)) }
        assertThat(store.get(keys.first())).isNotNull()

        store.put(keys.last(), grant(keys.last().trackId))

        assertThat(store.get(keys.first())).isNotNull()
        assertThat(store.get(keys[1])).isNull()
        assertThat(store.get(keys.last())).isNotNull()
    }

    private fun key(index: Int) = PlaybackGrantKey(
        ownerUserId = "owner",
        sessionId = "session",
        serverGeneration = 0,
        trackId = "track-$index",
        preferredQuality = PreferredQuality.AUTO,
        acceptedCodecs = emptyList(),
    )

    private fun grant(trackId: String) = PlaybackGrant(
        trackId = trackId,
        variantId = "variant-$trackId",
        selectedQuality = PreferredQuality.AUTO,
        signedUrl = "https://music.example/$trackId",
        expiresAtEpochMillis = Long.MAX_VALUE,
        mimeType = "audio/mp4",
        codec = "aac",
        container = "m4a",
        bitrate = 256_000,
        sampleRate = 48_000,
        contentLength = 1_024,
        checksumSha256 = null,
        cacheKey = "cache-$trackId",
    )
}
