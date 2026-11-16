package com.xymusic.app.feature.player.service

import android.content.Context
import android.os.Bundle
import androidx.media3.common.MediaItem
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.session.MediaSession
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.player.adapter.media3.PlaybackMediaUri
import com.xymusic.app.feature.player.domain.PlaybackGrant
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.model.PlayerFailure
import java.util.UUID
import java.util.concurrent.ExecutionException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertThrows
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@UnstableApi
class PlaybackMediaSessionCallbackTest {
    private val context: Context
        get() = ApplicationProvider.getApplicationContext()

    @Test
    fun onSetMediaItemsWaitsForInitialSessionReadyBeforeCompleting() = runTest(UnconfinedTestDispatcher()) {
        val initialSessionReady = CompletableDeferred<Unit>()
        withCallbackHarness(initialSessionReady) { session, callback ->
            val items = listOf(validMediaItem())
            val future =
                callback.onSetMediaItems(
                    mediaSession = session,
                    controller = appControllerInfo(),
                    mediaItems = items,
                    startIndex = 0,
                    startPositionMs = 42_000L,
                )

            assertThat(future.isDone).isFalse()

            initialSessionReady.complete(Unit)

            assertThat(future.isDone).isTrue()
            val resolved = future.get()
            assertThat(resolved.mediaItems).containsExactlyElementsIn(items).inOrder()
            assertThat(resolved.startIndex).isEqualTo(0)
            assertThat(resolved.startPositionMs).isEqualTo(42_000L)
        }
    }

    @Test
    fun onSetMediaItemsCompletesImmediatelyWhenSessionIsAlreadyReady() = runTest(UnconfinedTestDispatcher()) {
        val initialSessionReady = CompletableDeferred(Unit)
        withCallbackHarness(initialSessionReady) { session, callback ->
            val items = listOf(validMediaItem())
            val future =
                callback.onSetMediaItems(
                    mediaSession = session,
                    controller = appControllerInfo(),
                    mediaItems = items,
                    startIndex = 0,
                    startPositionMs = 0L,
                )

            assertThat(future.isDone).isTrue()
            assertThat(future.get().mediaItems).containsExactlyElementsIn(items).inOrder()
        }
    }

    @Test
    fun onSetMediaItemsRejectsInvalidMediaItems() = runTest(UnconfinedTestDispatcher()) {
        val initialSessionReady = CompletableDeferred(Unit)
        withCallbackHarness(initialSessionReady) { session, callback ->
            val invalidItem = MediaItem.Builder().setMediaId("queue-1").setUri("https://example.com/a.mp3").build()
            val future =
                callback.onSetMediaItems(
                    mediaSession = session,
                    controller = appControllerInfo(),
                    mediaItems = listOf(invalidItem),
                    startIndex = 0,
                    startPositionMs = 0L,
                )

            assertThat(future.isDone).isTrue()
            assertThrows(ExecutionException::class.java) { future.get() }
        }
    }

    private inline fun TestScope.withCallbackHarness(
        initialSessionReady: CompletableDeferred<Unit>,
        block: (MediaSession, PlaybackMediaSessionCallback) -> Unit,
    ) {
        val player = ExoPlayer.Builder(context).build()
        var session: MediaSession? = null
        try {
            val sleepTimerController =
                PlaybackSleepTimerController(
                    serviceScope = backgroundScope,
                    player = player,
                    mediaSession = { requireNotNull(session) },
                )
            val reloadCoordinator =
                PlaybackMediaReloadCoordinator(
                    player = player,
                    grantRepository = UnusedGrantRepository,
                    scope = backgroundScope,
                )
            val callback =
                PlaybackMediaSessionCallback(
                    appPackageName = context.packageName,
                    player = player,
                    serviceScope = backgroundScope,
                    initialSessionReady = initialSessionReady,
                    sleepTimerController = sleepTimerController,
                    mediaReloadCoordinator = reloadCoordinator,
                )
            session =
                MediaSession
                    .Builder(context, player)
                    .setId("callback-test-${UUID.randomUUID()}")
                    .setCallback(callback)
                    .build()
            block(session, callback)
        } finally {
            session?.release()
            player.release()
        }
    }

    private fun appControllerInfo(): MediaSession.ControllerInfo =
        MediaSession.ControllerInfo.createTestOnlyControllerInfo(
            context.packageName,
            0,
            0,
            MediaSession.ControllerInfo.LEGACY_CONTROLLER_INTERFACE_VERSION,
            MediaSession.ControllerInfo.LEGACY_CONTROLLER_VERSION,
            true,
            Bundle.EMPTY,
            true,
        )

    private fun validMediaItem(): MediaItem = MediaItem
        .Builder()
        .setMediaId("queue-1")
        .setUri(PlaybackMediaUri.forTrack("00000000-0000-0000-0000-000000000001"))
        .build()

    private object UnusedGrantRepository : PlaybackGrantRepository {
        override suspend fun get(trackId: String, forceRefresh: Boolean): PlayerResult<PlaybackGrant> =
            PlayerResult.Failure(PlayerFailure.PlaybackUnavailable)

        override fun invalidate(trackId: String) = Unit

        override fun clear() = Unit
    }
}
