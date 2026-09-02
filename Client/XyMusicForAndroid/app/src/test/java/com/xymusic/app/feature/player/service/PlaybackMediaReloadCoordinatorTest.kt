@file:Suppress("DEPRECATION")

package com.xymusic.app.feature.player.service

import android.os.Bundle
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.player.adapter.media3.PlaybackMediaMetadata
import com.xymusic.app.feature.player.adapter.media3.PlaybackMediaUri
import com.xymusic.app.feature.player.adapter.media3.globalPlaybackPositionMs
import com.xymusic.app.feature.player.adapter.media3.playbackRequestedStartPositionMs
import com.xymusic.app.feature.player.adapter.media3.playbackSourceOffsetMs
import com.xymusic.app.feature.player.adapter.media3.playbackStreamProtocol
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import com.xymusic.app.feature.player.domain.PlaybackStreamProtocol
import java.lang.reflect.Proxy
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@UnstableApi
class PlaybackMediaReloadCoordinatorTest {
    private val testDispatcher = StandardTestDispatcher()

    @Before
    fun setUp() {
        Dispatchers.setMain(testDispatcher)
    }

    @After
    fun tearDown() {
        Dispatchers.resetMain()
    }

    @Test
    fun hlsSeekRepreparesWithTheRequestedPositionAsTheImmediateSourceOffset() = runTest {
        val player = RecordingHlsPlayer()
        val coordinator =
            PlaybackMediaReloadCoordinator(
                player = player.delegate,
                grantRepository = RecordingGrantRepository(),
                scope = this,
            )

        val result = coordinator.seekTo(QUEUE_ITEM_ID, TARGET_POSITION_MS)
        assertThat(result).isTrue()

        assertThat(player.replacedItems).hasSize(1)
        val requestItem = player.replacedItems.single()
        assertThat(requestItem.playbackStreamProtocol()).isEqualTo(PlaybackStreamProtocol.HLS)
        // The offset must be declared with the request so no consumer ever
        // observes a transient global position of 0 during the resolve window.
        assertThat(requestItem.playbackSourceOffsetMs()).isEqualTo(TARGET_POSITION_MS)
        assertThat(requestItem.globalPlaybackPositionMs(0)).isEqualTo(TARGET_POSITION_MS)
        assertThat(requestItem.playbackRequestedStartPositionMs()).isEqualTo(TARGET_POSITION_MS)
        assertThat(player.stopCount).isEqualTo(1)
        assertThat(player.prepareCount).isEqualTo(1)
        assertThat(player.playWhenReady).isTrue()
    }

    @Test
    fun ensureHlsSeekPreservesPlayIntentDuringReprepare() = runTest {
        val player = RecordingHlsPlayer()
        player.playWhenReady = true
        val coordinator =
            PlaybackMediaReloadCoordinator(
                player = player.delegate,
                grantRepository = RecordingGrantRepository(),
                scope = this,
            )

        coordinator.seekTo(QUEUE_ITEM_ID, TARGET_POSITION_MS)

        assertThat(player.playWhenReady).isTrue()
    }

    @Test
    fun hlsReloadAtOrPastTheTrackEndClampsTheRequestedStartPosition() = runTest {
        val player = RecordingHlsPlayer()
        val coordinator =
            PlaybackMediaReloadCoordinator(
                player = player.delegate,
                grantRepository = RecordingGrantRepository(),
                scope = this,
            )

        val result = coordinator.seekTo(QUEUE_ITEM_ID, TRACK_DURATION_MS)
        assertThat(result).isTrue()

        val requestItem = player.replacedItems.single()
        // The server rejects a directed start at or past durationMs; the clamp
        // keeps the requested start inside the transcodeable window so a reload
        // racing the end of the track cannot produce an empty timeline.
        assertThat(requestItem.playbackSourceOffsetMs())
            .isEqualTo(TRACK_DURATION_MS - RELOAD_TAIL_MARGIN_MS)
        assertThat(requestItem.playbackRequestedStartPositionMs())
            .isEqualTo(TRACK_DURATION_MS - RELOAD_TAIL_MARGIN_MS)
    }

    private class RecordingGrantRepository : PlaybackGrantRepository {
        override suspend fun get(
            trackId: String,
            preferredQuality: com.xymusic.app.feature.player.domain.model.PreferredQuality,
            acceptedCodecs: List<String>,
            forceRefresh: Boolean,
            streamProtocol: PlaybackStreamProtocol?,
            startPositionMs: Long,
        ): com.xymusic.app.feature.player.domain.PlayerResult<com.xymusic.app.feature.player.domain.PlaybackGrant> =
            error("Not used")

        override fun invalidate(trackId: String) = Unit

        override fun clear() = Unit
    }

    private class RecordingHlsPlayer {
        val replacedItems = mutableListOf<MediaItem>()
        var stopCount = 0
        var prepareCount = 0
        var playWhenReady = true
        private var currentMediaItemIndex = 0
        private val mediaItems = mutableListOf(mediaItem(QUEUE_ITEM_ID))

        val delegate: Player =
            Proxy.newProxyInstance(
                Player::class.java.classLoader,
                arrayOf(Player::class.java),
            ) { _, method, args ->
                when (method.name) {
                    "getCurrentMediaItemIndex" -> currentMediaItemIndex
                    "getCurrentMediaItem" -> mediaItems.getOrNull(currentMediaItemIndex)
                    "getMediaItemCount" -> mediaItems.size
                    "getMediaItemAt" -> mediaItems[args!![0] as Int]
                    "getPlayWhenReady" -> playWhenReady
                    "setPlayWhenReady" -> {
                        playWhenReady = args!![0] as Boolean
                        Unit
                    }
                    "stop" -> {
                        stopCount += 1
                        Unit
                    }
                    "replaceMediaItem" -> {
                        mediaItems[args[0] as Int] = args[1] as MediaItem
                        replacedItems += args[1] as MediaItem
                        Unit
                    }
                    "seekTo" -> Unit
                    "prepare" -> {
                        prepareCount += 1
                        Unit
                    }
                    "toString" -> "RecordingHlsPlayer"
                    "hashCode" -> System.identityHashCode(this)
                    "equals" -> args?.firstOrNull() === this
                    else -> defaultValue(method.returnType)
                }
            } as Player
    }

    private companion object {
        const val QUEUE_ITEM_ID = "queue-1"
        const val TRACK_ID = "11111111-1111-1111-1111-111111111111"
        const val TARGET_POSITION_MS = 60_000L
        const val TRACK_DURATION_MS = 180_000L
        const val RELOAD_TAIL_MARGIN_MS = 500L

        fun mediaItem(queueItemId: String): MediaItem = MediaItem
            .Builder()
            .setMediaId(queueItemId)
            .setUri(PlaybackMediaUri.forTrack(TRACK_ID))
            .setMediaMetadata(
                MediaMetadata
                    .Builder()
                    .setExtras(
                        Bundle().apply {
                            putString(PlaybackMediaMetadata.EXTRA_TRACK_ID, TRACK_ID)
                            putString(PlaybackMediaMetadata.EXTRA_STREAM_PROTOCOL, "HLS")
                            putLong(PlaybackMediaMetadata.EXTRA_DURATION_MS, TRACK_DURATION_MS)
                        },
                    ).build(),
            ).build()

        fun defaultValue(type: Class<*>): Any? = when (type) {
            java.lang.Boolean.TYPE -> false
            java.lang.Byte.TYPE -> 0.toByte()
            java.lang.Short.TYPE -> 0.toShort()
            java.lang.Integer.TYPE -> 0
            java.lang.Long.TYPE -> 0L
            java.lang.Float.TYPE -> 0f
            java.lang.Double.TYPE -> 0.0
            java.lang.Character.TYPE -> '\u0000'
            else -> null
        }
    }
}
