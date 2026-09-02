@file:Suppress("DEPRECATION")

package com.xymusic.app.feature.player.service

import android.os.Bundle
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSpec
import androidx.media3.datasource.HttpDataSource
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.player.adapter.media3.PlaybackMediaMetadata
import com.xymusic.app.feature.player.adapter.media3.PlaybackMediaUri
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
@UnstableApi
class PlaybackGrantRecoveryControllerTest {
    @Test
    fun expiredGrantRefreshesTheCurrentItemAndPreservesPositionAndPlayIntent() {
        val player = RecordingPlayer(currentPositionMs = 12_345, playWhenReady = true)
        val repository = RecordingGrantRepository()
        val controller = PlaybackGrantRecoveryController(player.delegate, repository)

        controller.onPlayWhenReadyChanged(true, 0)
        player.playWhenReady = false
        controller.onPlayerError(expiredGrantError())

        assertThat(repository.invalidatedTrackIds).containsExactly(TRACK_ID)
        assertThat(player.seekCalls).containsExactly(GrantRecoverySeekCall(mediaItemIndex = 0, positionMs = 12_345))
        assertThat(player.prepareCount).isEqualTo(1)
        assertThat(player.playWhenReady).isTrue()
    }

    @Test
    fun repeatedFailureIsRateLimitedUntilThePlayerSuccessfullyStartsAgain() {
        val player = RecordingPlayer()
        val repository = RecordingGrantRepository()
        val controller = PlaybackGrantRecoveryController(player.delegate, repository)
        val error = expiredGrantError()

        controller.onPlayerError(error)
        controller.onPlayerError(error)
        assertThat(player.prepareCount).isEqualTo(1)
        assertThat(repository.invalidatedTrackIds).containsExactly(TRACK_ID)

        controller.onIsPlayingChanged(true)
        controller.onPlayerError(error)
        assertThat(player.prepareCount).isEqualTo(2)
        assertThat(repository.invalidatedTrackIds).containsExactly(TRACK_ID, TRACK_ID)
    }

    @Test
    fun unrelatedPlaybackErrorsAreIgnored() {
        val player = RecordingPlayer()
        val repository = RecordingGrantRepository()
        val controller = PlaybackGrantRecoveryController(player.delegate, repository)

        controller.onPlayerError(
            PlaybackException(
                "decoder failed",
                IllegalStateException(),
                PlaybackException.ERROR_CODE_DECODING_FAILED,
            ),
        )

        assertThat(player.prepareCount).isEqualTo(0)
        assertThat(repository.invalidatedTrackIds).isEmpty()
    }

    @Test
    fun serverTranscodeFailuresAreNotTreatedAsExpiredGrants() {
        val player = RecordingPlayer()
        val repository = RecordingGrantRepository()
        val controller = PlaybackGrantRecoveryController(player.delegate, repository)

        controller.onPlayerError(
            PlaybackException(
                "transcode failed",
                httpStatusError(500, "Internal Server Error"),
                PlaybackException.ERROR_CODE_IO_BAD_HTTP_STATUS,
            ),
        )

        assertThat(player.prepareCount).isEqualTo(0)
        assertThat(repository.invalidatedTrackIds).isEmpty()
    }

    private fun expiredGrantError() = PlaybackException(
        "playback grant expired",
        httpStatusError(404, "Playback stream was not found or expired"),
        PlaybackException.ERROR_CODE_IO_BAD_HTTP_STATUS,
    )

    private fun httpStatusError(code: Int, message: String) = HttpDataSource.InvalidResponseCodeException(
        code,
        message,
        null,
        emptyMap<String, List<String>>(),
        DataSpec.Builder().setUri("https://example.com/streams/session/index.m3u8").build(),
        ByteArray(0),
    )

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

    private class RecordingPlayer(
        var currentMediaItemIndex: Int = 0,
        var currentPositionMs: Long = 4_000,
        var playWhenReady: Boolean = false,
    ) {
        val seekCalls = mutableListOf<GrantRecoverySeekCall>()
        var prepareCount = 0
        private val mediaItem =
            MediaItem
                .Builder()
                .setMediaId("queue-1")
                .setUri(PlaybackMediaUri.forTrack(TRACK_ID))
                .setMediaMetadata(
                    MediaMetadata
                        .Builder()
                        .setExtras(
                            Bundle().apply {
                                putString(PlaybackMediaMetadata.EXTRA_TRACK_ID, TRACK_ID)
                            },
                        ).build(),
                ).build()

        val delegate: Player =
            Proxy.newProxyInstance(
                Player::class.java.classLoader,
                arrayOf(Player::class.java),
            ) { _, method, args ->
                when (method.name) {
                    "getCurrentMediaItemIndex" -> currentMediaItemIndex
                    "getCurrentPosition" -> currentPositionMs
                    "getPlayWhenReady" -> playWhenReady
                    "getMediaItemCount" -> 1
                    "getMediaItemAt" -> mediaItem
                    "setPlayWhenReady" -> {
                        playWhenReady = args!![0] as Boolean
                        Unit
                    }
                    "seekTo" -> {
                        currentMediaItemIndex = args!![0] as Int
                        currentPositionMs = args[1] as Long
                        seekCalls += GrantRecoverySeekCall(currentMediaItemIndex, currentPositionMs)
                        Unit
                    }
                    "prepare" -> {
                        prepareCount += 1
                        Unit
                    }
                    "toString" -> "RecordingPlayer"
                    "hashCode" -> System.identityHashCode(this)
                    "equals" -> args?.firstOrNull() === this
                    else -> defaultValue(method.returnType)
                }
            } as Player
    }

    private companion object {
        const val TRACK_ID = "11111111-1111-1111-1111-111111111111"

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

private data class GrantRecoverySeekCall(val mediaItemIndex: Int, val positionMs: Long)
