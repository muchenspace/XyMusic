package com.xymusic.app.feature.player.service

import android.app.Application
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.player.domain.PlaybackEventSink
import com.xymusic.app.feature.player.domain.PlaybackQueueStore
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.StoredPlaybackQueueItem
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
        firstPlayer.currentPositionMs = 2_750
        var flushed = false

        firstController.flushForTaskRemoval(stopAfterFlush = true) {
            assertThat(store.replaceCount).isEqualTo(1)
            flushed = true
        }
        advanceUntilIdle()

        assertThat(flushed).isTrue()
        assertThat(store.items.single(StoredPlaybackQueueItem::isCurrent).resumePositionMs)
            .isEqualTo(2_750)

        val restoredPlayer = RecordingPlayer()
        val restoredController = controller(restoredPlayer, store)
        restoredController.clearForAccountChange(USER_ID)
        restoredController.restoreQueue()

        assertThat(restoredPlayer.mediaItems.map(MediaItem::mediaId))
            .containsExactly("queue-1", "queue-2")
            .inOrder()
        assertThat(restoredPlayer.currentMediaItemIndex).isEqualTo(0)
        assertThat(restoredPlayer.currentPositionMs).isEqualTo(2_750)
        assertThat(restoredPlayer.playWhenReady).isFalse()
    }

    private fun kotlinx.coroutines.test.TestScope.controller(player: RecordingPlayer, store: PlaybackQueueStore) =
        PlaybackPersistenceController(
            player = player.delegate,
            serviceScope = this,
            queueStore = store,
            eventSink = PlaybackEventSink { _, _ -> Unit },
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

    private class RecordingPlayer {
        val mediaItems = mutableListOf<MediaItem>()
        var currentMediaItemIndex = 0
        var currentPositionMs = 0L
        var playWhenReady = false

        val delegate: Player =
            Proxy.newProxyInstance(
                Player::class.java.classLoader,
                arrayOf(Player::class.java),
            ) { _, method, args ->
                when (method.name) {
                    "addListener", "removeListener", "stop", "prepare", "release" -> Unit
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
                    "isPlaying" -> false
                    "getMediaItemCount" -> mediaItems.size
                    "getMediaItemAt" -> mediaItems[args!![0] as Int]
                    "getCurrentMediaItemIndex" -> currentMediaItemIndex
                    "getCurrentMediaItem" -> mediaItems.getOrNull(currentMediaItemIndex)
                    "getCurrentPosition", "getContentPosition" -> currentPositionMs
                    "getDuration" -> 30_000L
                    "toString" -> "RecordingPlayer"
                    "hashCode" -> System.identityHashCode(this)
                    "equals" -> args?.firstOrNull() === this
                    else -> defaultValue(method.returnType)
                }
            } as Player
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
