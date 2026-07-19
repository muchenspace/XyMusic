@file:Suppress("DEPRECATION")

package com.xymusic.app.feature.player.service

import android.os.Bundle
import androidx.media3.common.C
import androidx.media3.common.Format
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.MimeTypes
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlaybackException
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.player.data.media.PlaybackMediaMetadata
import com.xymusic.app.feature.player.data.media.PlaybackMediaUri
import com.xymusic.app.feature.player.domain.PlaybackGrant
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.model.PreferredQuality
import java.io.IOException
import java.lang.reflect.Proxy
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

@RunWith(RobolectricTestRunner::class)
@UnstableApi
class PlaybackCodecFallbackControllerTest {
    @Test
    fun flacDecoderInitAndDecodingErrorsApplyFallbackAndPreservePlaybackState() {
        listOf(
            PlaybackException.ERROR_CODE_DECODER_INIT_FAILED,
            PlaybackException.ERROR_CODE_DECODING_FAILED,
        ).forEach { errorCode ->
            val player = RecordingPlayer(currentMediaItemIndex = 1, currentPositionMs = 12_345, playWhenReady = true)
            val repository = RecordingGrantRepository(fallbackEnabled = true)
            var broadcasts = 0
            val controller = controller(player, repository) { broadcasts += 1 }

            controller.onPlayerError(rendererError(MimeTypes.AUDIO_FLAC, errorCode))

            assertThat(repository.fallbackTrackIds).containsExactly(TRACK_2)
            assertThat(player.prepareCount).isEqualTo(1)
            assertThat(player.seekCalls).containsExactly(SeekCall(mediaItemIndex = 1, positionMs = 12_345))
            assertThat(player.currentMediaItemIndex).isEqualTo(1)
            assertThat(player.currentPositionMs).isEqualTo(12_345)
            assertThat(player.playWhenReady).isTrue()
            assertThat(broadcasts).isEqualTo(1)
        }
    }

    @Test
    fun nonMatchingPlaybackErrorsDoNotApplyFallback() {
        val errors =
            listOf(
                PlaybackException(
                    "plain playback error",
                    IllegalStateException(),
                    PlaybackException.ERROR_CODE_DECODING_FAILED,
                ),
                ExoPlaybackException.createForSource(
                    IOException("source failed"),
                    PlaybackException.ERROR_CODE_IO_UNSPECIFIED,
                ),
                rendererError(MimeTypes.AUDIO_AAC, PlaybackException.ERROR_CODE_DECODING_FAILED),
                rendererError(MimeTypes.AUDIO_FLAC, PlaybackException.ERROR_CODE_IO_UNSPECIFIED),
                rendererError(
                    MimeTypes.AUDIO_FLAC,
                    PlaybackException.ERROR_CODE_DECODING_FORMAT_UNSUPPORTED,
                ),
            )

        errors.forEach { error ->
            val player = RecordingPlayer()
            val repository = RecordingGrantRepository(fallbackEnabled = true)
            var broadcasts = 0

            controller(player, repository) { broadcasts += 1 }.onPlayerError(error)

            assertThat(repository.fallbackTrackIds).isEmpty()
            assertThat(player.prepareCount).isEqualTo(0)
            assertThat(player.seekCalls).isEmpty()
            assertThat(broadcasts).isEqualTo(0)
        }
    }

    @Test
    fun eachQueueItemCanApplyFallbackOnlyOnce() {
        val player = RecordingPlayer()
        val repository = RecordingGrantRepository(fallbackEnabled = true)
        var broadcasts = 0
        val controller = controller(player, repository) { broadcasts += 1 }
        val error = rendererError(MimeTypes.AUDIO_FLAC, PlaybackException.ERROR_CODE_DECODING_FAILED)

        controller.onPlayerError(error)
        controller.onPlayerError(error)
        player.currentMediaItemIndex = 1
        player.currentPositionMs = 8_000
        controller.onPlayerError(error)
        controller.onPlayerError(error)

        assertThat(repository.fallbackTrackIds).containsExactly(TRACK_1, TRACK_2).inOrder()
        assertThat(player.prepareCount).isEqualTo(2)
        assertThat(player.seekCalls)
            .containsExactly(
                SeekCall(mediaItemIndex = 0, positionMs = 4_000),
                SeekCall(mediaItemIndex = 1, positionMs = 8_000),
            ).inOrder()
        assertThat(broadcasts).isEqualTo(1)
    }

    @Test
    fun rejectedRepositoryFallbackIsNotPreparedOrRetried() {
        val player = RecordingPlayer()
        val repository = RecordingGrantRepository(enableResult = false, fallbackEnabled = false)
        var broadcasts = 0
        val controller = controller(player, repository) { broadcasts += 1 }
        val error = rendererError(MimeTypes.AUDIO_FLAC, PlaybackException.ERROR_CODE_DECODER_INIT_FAILED)

        controller.onPlayerError(error)
        controller.onPlayerError(error)

        assertThat(repository.fallbackTrackIds).containsExactly(TRACK_1)
        assertThat(player.prepareCount).isEqualTo(0)
        assertThat(player.seekCalls).isEmpty()
        assertThat(broadcasts).isEqualTo(0)
    }

    @Test
    fun existingTrackFallbackStillRepreparesAStaleFlacQueueItemOnce() {
        val player = RecordingPlayer(currentPositionMs = -5, playWhenReady = false)
        val repository = RecordingGrantRepository(enableResult = false, fallbackEnabled = true)
        var broadcasts = 0
        val controller = controller(player, repository) { broadcasts += 1 }
        val error = rendererError(MimeTypes.AUDIO_FLAC, PlaybackException.ERROR_CODE_DECODING_FAILED)

        controller.onPlayerError(error)
        controller.onPlayerError(error)

        assertThat(repository.fallbackTrackIds).containsExactly(TRACK_1)
        assertThat(player.seekCalls).containsExactly(SeekCall(mediaItemIndex = 0, positionMs = 0))
        assertThat(player.prepareCount).isEqualTo(1)
        assertThat(player.playWhenReady).isFalse()
        assertThat(broadcasts).isEqualTo(1)
    }

    @Test
    fun accountChangeResetAllowsOneFallbackNotificationForTheNewSession() {
        val player = RecordingPlayer()
        val repository = RecordingGrantRepository(fallbackEnabled = true)
        var broadcasts = 0
        val controller = controller(player, repository) { broadcasts += 1 }
        val error = rendererError(MimeTypes.AUDIO_FLAC, PlaybackException.ERROR_CODE_DECODING_FAILED)

        controller.onPlayerError(error)
        controller.resetForAccountChange()
        controller.onPlayerError(error)

        assertThat(player.prepareCount).isEqualTo(2)
        assertThat(broadcasts).isEqualTo(2)
    }

    private fun controller(
        player: RecordingPlayer,
        repository: PlaybackGrantRepository,
        onFallbackApplied: () -> Unit,
    ) = PlaybackCodecFallbackController(
        player = player.delegate,
        grantRepository = repository,
        onFallbackApplied = onFallbackApplied,
    )

    private class RecordingGrantRepository(
        private val enableResult: Boolean = true,
        private val fallbackEnabled: Boolean,
    ) : PlaybackGrantRepository {
        val fallbackTrackIds = mutableListOf<String>()

        override suspend fun get(
            trackId: String,
            preferredQuality: PreferredQuality,
            acceptedCodecs: List<String>,
            forceRefresh: Boolean,
        ): PlayerResult<PlaybackGrant> = error("Not used")

        override fun invalidate(trackId: String) = Unit

        override fun enableCompatibleCodecFallback(trackId: String): Boolean {
            fallbackTrackIds += trackId
            return enableResult
        }

        override fun isCompatibleCodecFallbackEnabled(trackId: String): Boolean = fallbackEnabled

        override fun clear() = Unit
    }

    private class RecordingPlayer(
        var currentMediaItemIndex: Int = 0,
        var currentPositionMs: Long = 4_000,
        var playWhenReady: Boolean = false,
    ) {
        val mediaItems = listOf(mediaItem("queue-1", TRACK_1), mediaItem("queue-2", TRACK_2))
        val seekCalls = mutableListOf<SeekCall>()
        var prepareCount = 0

        val delegate: Player =
            Proxy.newProxyInstance(
                Player::class.java.classLoader,
                arrayOf(Player::class.java),
            ) { _, method, args ->
                when (method.name) {
                    "getCurrentMediaItemIndex" -> currentMediaItemIndex
                    "getCurrentPosition" -> currentPositionMs
                    "getPlayWhenReady" -> playWhenReady
                    "getMediaItemCount" -> mediaItems.size
                    "getMediaItemAt" -> mediaItems[args!![0] as Int]
                    "setPlayWhenReady" -> {
                        playWhenReady = args!![0] as Boolean
                        Unit
                    }
                    "seekTo" -> {
                        currentMediaItemIndex = args!![0] as Int
                        currentPositionMs = args[1] as Long
                        seekCalls += SeekCall(currentMediaItemIndex, currentPositionMs)
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
        const val TRACK_1 = "11111111-1111-1111-1111-111111111111"
        const val TRACK_2 = "22222222-2222-2222-2222-222222222222"

        fun mediaItem(queueItemId: String, trackId: String): MediaItem = MediaItem
            .Builder()
            .setMediaId(queueItemId)
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

        fun rendererError(sampleMimeType: String, errorCode: Int): ExoPlaybackException =
            ExoPlaybackException.createForRenderer(
                IllegalStateException("decoder failed"),
                "AudioRenderer",
                0,
                Format.Builder().setSampleMimeType(sampleMimeType).build(),
                C.FORMAT_HANDLED,
                false,
                errorCode,
            )

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

private data class SeekCall(val mediaItemIndex: Int, val positionMs: Long)
