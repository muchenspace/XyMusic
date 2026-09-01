package com.xymusic.app.feature.player.service

import android.app.Application
import android.os.Bundle
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.player.adapter.media3.PlaybackMediaMetadata
import com.xymusic.app.feature.player.domain.PlaybackEventSink
import com.xymusic.app.feature.player.domain.PlaybackModePreference
import com.xymusic.app.feature.player.domain.PlaybackModeStore
import com.xymusic.app.feature.player.domain.PlaybackQueueStore
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.StoredPlaybackQueueItem
import com.xymusic.app.feature.player.domain.model.RepeatMode
import java.lang.reflect.Proxy
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
@OptIn(ExperimentalCoroutinesApi::class)
class PlaybackPersistenceControllerTest {
    @Test
    fun taskRemovalFlushPersistsQueueBeforeCallbackAndRestoresAfterProcessDeath() = runTest {
        val store = InMemoryPlaybackQueueStore(initialItems = storedQueue(resumePositionMs = 1_500))
        val firstPlayer = RecordingPlayer()
        val firstController = controller(firstPlayer, store)
        firstController.clearForAccountChange(USER_ID)
        firstController.restoreQueue()
        firstPlayer.setSourceOffset(10_000)
        firstPlayer.currentPositionMs = 2_750
        var flushed = false

        firstController.flushForTaskRemoval(stopAfterFlush = true) {
            assertThat(store.replaceCount).isEqualTo(1)
            flushed = true
        }
        advanceUntilIdle()

        assertThat(flushed).isTrue()
        assertThat(store.items.single(StoredPlaybackQueueItem::isCurrent).resumePositionMs)
            .isEqualTo(12_750)

        val restoredPlayer = RecordingPlayer()
        val restoredController = controller(restoredPlayer, store)
        restoredController.clearForAccountChange(USER_ID)
        restoredController.restoreQueue()

        assertThat(restoredPlayer.mediaItems.map(MediaItem::mediaId))
            .containsExactly("queue-1", "queue-2")
            .inOrder()
        assertThat(restoredPlayer.currentMediaItemIndex).isEqualTo(0)
        assertThat(restoredPlayer.currentPositionMs).isEqualTo(12_750)
        assertThat(restoredPlayer.playWhenReady).isFalse()
    }

    @Test
    fun checkpointsUseGlobalPositionForAResolvedOffsetMediaItem() = runTest {
        val store = InMemoryPlaybackQueueStore(initialItems = storedQueue(resumePositionMs = 0))
        val player = RecordingPlayer()
        val checkpoints = mutableListOf<com.xymusic.app.feature.player.domain.PlaybackCheckpoint>()
        val controller = controller(
            player = player,
            store = store,
            eventSink = PlaybackEventSink { _, checkpoint -> checkpoints += checkpoint },
        )
        controller.clearForAccountChange(USER_ID)
        controller.restoreQueue()
        player.setSourceOffset(10_000)
        player.currentPositionMs = 2_000
        player.isPlaying = true
        player.listeners.forEach { it.onIsPlayingChanged(true) }
        player.isPlaying = false
        player.listeners.forEach { it.onIsPlayingChanged(false) }
        advanceUntilIdle()

        assertThat(checkpoints).isNotEmpty()
        assertThat(checkpoints.first().positionMs).isEqualTo(12_000)
        assertThat(checkpoints.first().durationMs).isEqualTo(30_000)
    }

    @Test
    fun playbackModeSurvivesControllerRecreationAndModeChangesArePersisted() = runTest {
        val store = InMemoryPlaybackQueueStore(initialItems = storedQueue(resumePositionMs = 0))
        val modeStore = InMemoryPlaybackModeStore(
            PlaybackModePreference(
                repeatMode = RepeatMode.ONE,
                shuffleEnabled = true,
            ),
        )
        val player = RecordingPlayer()
        val controller = controller(player, store, modeStore)

        controller.clearForAccountChange(USER_ID)
        controller.restoreQueue()

        assertThat(player.delegate.repeatMode).isEqualTo(Player.REPEAT_MODE_ONE)
        assertThat(player.delegate.shuffleModeEnabled).isTrue()

        player.delegate.repeatMode = Player.REPEAT_MODE_ALL
        player.delegate.shuffleModeEnabled = false
        advanceUntilIdle()

        assertThat(modeStore.preference).isEqualTo(
            PlaybackModePreference(
                repeatMode = RepeatMode.ALL,
                shuffleEnabled = false,
            ),
        )
    }

    private fun kotlinx.coroutines.test.TestScope.controller(
        player: RecordingPlayer,
        store: PlaybackQueueStore,
        modeStore: PlaybackModeStore = InMemoryPlaybackModeStore(),
        eventSink: PlaybackEventSink = PlaybackEventSink { _, _ -> Unit },
    ) = PlaybackPersistenceController(
        player = player.delegate,
        serviceScope = this,
        queueStore = store,
        modeStore = modeStore,
        eventSink = eventSink,
        clock = Clock.fixed(Instant.ofEpochMilli(10_000), ZoneOffset.UTC),
        cancelSleepTimer = {},
        clearPlaybackGrants = {},
    )

    private class InMemoryPlaybackQueueStore(initialItems: List<StoredPlaybackQueueItem>) : PlaybackQueueStore {
        private val state = MutableStateFlow(initialItems)
        val items: List<StoredPlaybackQueueItem>
            get() = state.value
        var replaceCount = 0
            private set

        override fun observe(): Flow<List<StoredPlaybackQueueItem>> = state

        override suspend fun replace(ownerUserId: String, items: List<StoredPlaybackQueueItem>): PlayerResult<Unit> {
            assertThat(ownerUserId).isEqualTo(USER_ID)
            replaceCount += 1
            state.value = items
            return PlayerResult.Success(Unit)
        }

        override suspend fun updatePosition(
            ownerUserId: String,
            queueItemId: String,
            positionMs: Long,
        ): PlayerResult<Unit> {
            state.value = state.value.map { item ->
                if (item.queueItemId == queueItemId) item.copy(resumePositionMs = positionMs) else item
            }
            return PlayerResult.Success(Unit)
        }

        override suspend fun setCurrent(
            ownerUserId: String,
            queueItemId: String,
            positionMs: Long,
        ): PlayerResult<Unit> {
            state.value = state.value.map { item ->
                item.copy(
                    isCurrent = item.queueItemId == queueItemId,
                    resumePositionMs = if (item.queueItemId == queueItemId) positionMs else item.resumePositionMs,
                )
            }
            return PlayerResult.Success(Unit)
        }

        override suspend fun clear(ownerUserId: String): PlayerResult<Unit> {
            state.value = emptyList()
            return PlayerResult.Success(Unit)
        }
    }

    private class InMemoryPlaybackModeStore(var preference: PlaybackModePreference = PlaybackModePreference()) :
        PlaybackModeStore {
        override suspend fun read(): PlaybackModePreference = preference

        override suspend fun write(preference: PlaybackModePreference) {
            this.preference = preference
        }
    }

    private class RecordingPlayer {
        val mediaItems = mutableListOf<MediaItem>()
        val listeners = mutableListOf<Player.Listener>()
        var currentMediaItemIndex = 0
        var currentPositionMs = 0L
        var durationMs = 30_000L
        var playWhenReady = false
        var isPlaying = false
        var repeatMode = Player.REPEAT_MODE_ALL
        var shuffleModeEnabled = false

        val delegate: Player =
            Proxy.newProxyInstance(
                Player::class.java.classLoader,
                arrayOf(Player::class.java),
            ) { _, method, args ->
                when (method.name) {
                    "addListener" -> {
                        listeners += args!![0] as Player.Listener
                        Unit
                    }
                    "removeListener" -> {
                        listeners -= args!![0] as Player.Listener
                        Unit
                    }
                    "stop", "prepare", "release" -> Unit
                    "clearMediaItems" -> {
                        mediaItems.clear()
                        currentMediaItemIndex = 0
                        currentPositionMs = 0
                        Unit
                    }

                    "removeMediaItems" -> {
                        mediaItems.clear()
                        currentMediaItemIndex = 0
                        currentPositionMs = 0
                        Unit
                    }

                    "setMediaItems" -> {
                        @Suppress("UNCHECKED_CAST")
                        val items = args!![0] as List<MediaItem>
                        mediaItems.clear()
                        mediaItems += items
                        if (args.size == 3) {
                            currentMediaItemIndex = args[1] as Int
                            currentPositionMs = args[2] as Long
                        }
                        Unit
                    }

                    "setPlayWhenReady" -> {
                        playWhenReady = args!![0] as Boolean
                        Unit
                    }

                    "getPlayWhenReady" -> playWhenReady
                    "setRepeatMode" -> {
                        repeatMode = args!![0] as Int
                        listeners.forEach { it.onRepeatModeChanged(repeatMode) }
                        Unit
                    }
                    "getRepeatMode" -> repeatMode
                    "setShuffleModeEnabled" -> {
                        shuffleModeEnabled = args!![0] as Boolean
                        listeners.forEach { it.onShuffleModeEnabledChanged(shuffleModeEnabled) }
                        Unit
                    }
                    "getShuffleModeEnabled", "isShuffleModeEnabled" -> shuffleModeEnabled
                    "isPlaying" -> isPlaying
                    "getMediaItemCount" -> mediaItems.size
                    "getMediaItemAt" -> mediaItems[args!![0] as Int]
                    "getCurrentMediaItemIndex" -> currentMediaItemIndex
                    "getCurrentMediaItem" -> mediaItems.getOrNull(currentMediaItemIndex)
                    "getCurrentPosition", "getContentPosition" -> currentPositionMs
                    "getDuration" -> durationMs
                    "toString" -> "RecordingPlayer"
                    "hashCode" -> System.identityHashCode(this)
                    "equals" -> args?.firstOrNull() === this
                    else -> defaultValue(method.returnType)
                }
            } as Player

        fun setSourceOffset(offsetMs: Long) {
            val index = currentMediaItemIndex
            val item = mediaItems[index]
            val extras = Bundle(item.mediaMetadata.extras ?: Bundle()).apply {
                putLong(PlaybackMediaMetadata.EXTRA_SOURCE_OFFSET_MS, offsetMs)
            }
            durationMs = (30_000L - offsetMs).coerceAtLeast(0)
            mediaItems[index] = item
                .buildUpon()
                .setMediaMetadata(
                    item.mediaMetadata.buildUpon().setExtras(extras).build(),
                )
                .build()
        }
    }

    private companion object {
        const val USER_ID = "user-1"
        const val TRACK_1 = "11111111-1111-1111-1111-111111111111"
        const val TRACK_2 = "22222222-2222-2222-2222-222222222222"

        fun storedQueue(resumePositionMs: Long): List<StoredPlaybackQueueItem> = listOf(
            storedItem("queue-1", TRACK_1, position = 0, isCurrent = true, resumePositionMs),
            storedItem("queue-2", TRACK_2, position = 1, isCurrent = false, resumePositionMs = 0),
        )

        fun storedItem(
            queueItemId: String,
            trackId: String,
            position: Int,
            isCurrent: Boolean,
            resumePositionMs: Long,
        ) = StoredPlaybackQueueItem(
            queueItemId = queueItemId,
            position = position,
            trackId = trackId,
            variantId = null,
            stableCacheKey = null,
            resumePositionMs = resumePositionMs,
            isCurrent = isCurrent,
            enqueuedAtEpochMillis = 1_000L + position,
            title = "Track ${position + 1}",
            artistNames = listOf("Artist"),
            albumTitle = "Album",
            artworkUrl = null,
            artworkCacheKey = null,
            durationMs = 30_000,
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
