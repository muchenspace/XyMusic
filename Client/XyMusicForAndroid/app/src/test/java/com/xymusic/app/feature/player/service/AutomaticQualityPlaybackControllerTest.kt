package com.xymusic.app.feature.player.service

import android.os.Bundle
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.Player
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.domain.settings.StreamingQuality
import com.xymusic.app.feature.player.adapter.media3.PlaybackMediaMetadata
import com.xymusic.app.feature.player.adapter.media3.PlaybackMediaUri
import com.xymusic.app.feature.player.data.quality.AutomaticPlaybackQualityController
import com.xymusic.app.feature.player.domain.PlaybackGrant
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.model.PreferredQuality
import java.lang.reflect.Proxy
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class AutomaticQualityPlaybackControllerTest {
    @Test
    fun rebufferAfterPlaybackStartedReloadsTheCurrentItemAtTheLowerQuality() {
        val player = RecordingPlayer()
        val grants = RecordingGrantRepository()
        val quality = AutomaticPlaybackQualityController().apply {
            resolveTrackQuality(TRACK_ID, StreamingQuality.AUTO)
        }
        var nowMs = 20_000L
        val controller =
            AutomaticQualityPlaybackController(
                player = player.delegate,
                grantRepository = grants,
                qualityController = quality,
                elapsedRealtime = { nowMs },
            )

        controller.onMediaItemTransition(player.mediaItem, Player.MEDIA_ITEM_TRANSITION_REASON_PLAYLIST_CHANGED)
        controller.onIsPlayingChanged(true)
        controller.onPlaybackStateChanged(Player.STATE_BUFFERING)

        assertThat(grants.invalidatedTrackIds).containsExactly(TRACK_ID)
        assertThat(player.stopCount).isEqualTo(1)
        assertThat(player.seekCalls).containsExactly(SeekCall(0, 10_000))
        assertThat(player.prepareCount).isEqualTo(1)
        assertThat(player.playWhenReady).isTrue()
        assertThat(quality.resolveTrackQuality(TRACK_ID, StreamingQuality.AUTO))
            .isEqualTo(PreferredQuality.DATA_SAVER)

        nowMs += 1_000
        controller.onPlaybackStateChanged(Player.STATE_BUFFERING)
        assertThat(player.stopCount).isEqualTo(1)
    }

    @Test
    fun rebufferWithoutAValidCurrentItemDoesNotApplyADowngradePenalty() {
        val player = RecordingPlayer(mediaItemIndex = -1, itemCount = 0)
        val grants = RecordingGrantRepository()
        val quality = AutomaticPlaybackQualityController().apply {
            resolveTrackQuality(TRACK_ID, StreamingQuality.AUTO)
        }
        val controller =
            AutomaticQualityPlaybackController(
                player = player.delegate,
                grantRepository = grants,
                qualityController = quality,
                elapsedRealtime = { 20_000L },
            )

        controller.onMediaItemTransition(player.mediaItem, Player.MEDIA_ITEM_TRANSITION_REASON_PLAYLIST_CHANGED)
        controller.onIsPlayingChanged(true)
        controller.onPlaybackStateChanged(Player.STATE_BUFFERING)

        assertThat(grants.invalidatedTrackIds).isEmpty()
        assertThat(player.stopCount).isEqualTo(0)
        assertThat(quality.resolveTrackQuality(TRACK_ID, StreamingQuality.AUTO))
            .isEqualTo(PreferredQuality.STANDARD)
    }

    private class RecordingGrantRepository : PlaybackGrantRepository {
        val invalidatedTrackIds = mutableListOf<String>()

        override suspend fun get(
            trackId: String,
            preferredQuality: PreferredQuality,
            acceptedCodecs: List<String>,
            forceRefresh: Boolean,
            streamProtocol: com.xymusic.app.feature.player.domain.PlaybackStreamProtocol?,
            startPositionMs: Long,
        ): PlayerResult<PlaybackGrant> = error("Not used")

        override fun invalidate(trackId: String) {
            invalidatedTrackIds += trackId
        }

        override fun clear() = Unit
    }

    private class RecordingPlayer(private val mediaItemIndex: Int = 0, private val itemCount: Int = 1) {
        val mediaItem = mediaItem(TRACK_ID)
        val seekCalls = mutableListOf<SeekCall>()
        var currentPositionMs = 10_000L
        var playWhenReady = true
        var prepareCount = 0
        var stopCount = 0

        val delegate: Player =
            Proxy.newProxyInstance(
                Player::class.java.classLoader,
                arrayOf(Player::class.java),
            ) { _, method, args ->
                when (method.name) {
                    "getCurrentMediaItem" -> mediaItem
                    "getCurrentMediaItemIndex" -> mediaItemIndex
                    "getCurrentPosition" -> currentPositionMs
                    "getPlayWhenReady" -> playWhenReady
                    "getMediaItemCount" -> itemCount
                    "getMediaItemAt" -> mediaItem
                    "setPlayWhenReady" -> {
                        playWhenReady = args!![0] as Boolean
                        Unit
                    }
                    "seekTo" -> {
                        currentPositionMs = args!![1] as Long
                        seekCalls += SeekCall(args[0] as Int, currentPositionMs)
                        Unit
                    }
                    "prepare" -> {
                        prepareCount += 1
                        Unit
                    }
                    "stop" -> {
                        stopCount += 1
                        Unit
                    }
                    "toString" -> "RecordingPlayer"
                    "hashCode" -> System.identityHashCode(this)
                    "equals" -> args?.firstOrNull() === this
                    else -> defaultValue(method.returnType)
                }
            } as Player
    }

    private data class SeekCall(val index: Int, val positionMs: Long)

    private companion object {
        const val TRACK_ID = "11111111-1111-1111-1111-111111111111"

        fun mediaItem(trackId: String): MediaItem = MediaItem
            .Builder()
            .setMediaId("queue-1")
            .setUri(PlaybackMediaUri.forTrack(trackId))
            .setMediaMetadata(
                MediaMetadata
                    .Builder()
                    .setExtras(
                        Bundle().apply {
                            putString(PlaybackMediaMetadata.EXTRA_TRACK_ID, trackId)
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
