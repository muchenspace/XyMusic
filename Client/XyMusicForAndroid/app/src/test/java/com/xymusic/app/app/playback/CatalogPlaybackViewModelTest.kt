package com.xymusic.app.app.playback

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.ui.media.CatalogAlbumLinkUi
import com.xymusic.app.core.ui.media.CatalogArtistLinkUi
import com.xymusic.app.core.ui.media.CatalogTrackUi
import com.xymusic.app.feature.player.domain.PlayerRepository
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.PlayerUseCases
import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
import com.xymusic.app.feature.player.domain.model.PlayerState
import com.xymusic.app.feature.player.domain.model.RepeatMode
import com.xymusic.app.support.MainDispatcherRule
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Rule
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class CatalogPlaybackViewModelTest {
    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    @Test
    fun playQueuePreservesTrackOrderAndStartsFromSelectedTrack() = runTest {
        val repository = RecordingPlayerRepository()
        val tracks =
            listOf(
                track("track-1"),
                track("track-2"),
                track("track-3"),
            )
        val viewModel = CatalogPlaybackViewModel(PlayerUseCases(repository))

        viewModel.playQueue(tracks = tracks, startTrack = tracks[1])
        advanceUntilIdle()

        val request = requireNotNull(repository.lastSetQueueRequest)
        assertThat(request.items.map(PlayerQueueItem::trackId))
            .containsExactly("track-1", "track-2", "track-3")
            .inOrder()
        assertThat(request.items.single { it.queueItemId == request.startQueueItemId }.trackId)
            .isEqualTo("track-2")
        assertThat(request.playWhenReady).isTrue()
    }

    @Test
    fun playNowKeepsSingleTrackQueueBehavior() = runTest {
        val repository = RecordingPlayerRepository()
        val viewModel = CatalogPlaybackViewModel(PlayerUseCases(repository))

        viewModel.playNow(track("track-1"))
        advanceUntilIdle()

        val request = requireNotNull(repository.lastSetQueueRequest)
        assertThat(request.items.map(PlayerQueueItem::trackId)).containsExactly("track-1")
    }
}

private data class SetQueueRequest(
    val items: List<PlayerQueueItem>,
    val startQueueItemId: String?,
    val startPositionMs: Long,
    val playWhenReady: Boolean,
)

private class RecordingPlayerRepository : PlayerRepository {
    private val mutableState = MutableStateFlow(PlayerState())
    override val state: StateFlow<PlayerState> = mutableState

    var lastSetQueueRequest: SetQueueRequest? = null

    override suspend fun connect(): PlayerResult<Unit> = success()

    override suspend fun disconnect() = Unit

    override suspend fun setQueue(
        items: List<PlayerQueueItem>,
        startQueueItemId: String?,
        startPositionMs: Long,
        playWhenReady: Boolean,
    ): PlayerResult<Unit> {
        lastSetQueueRequest =
            SetQueueRequest(
                items = items,
                startQueueItemId = startQueueItemId,
                startPositionMs = startPositionMs,
                playWhenReady = playWhenReady,
            )
        return success()
    }

    override suspend fun addToQueue(items: List<PlayerQueueItem>): PlayerResult<Unit> = success()

    override suspend fun removeFromQueue(queueItemId: String): PlayerResult<Unit> = success()

    override suspend fun moveQueueItem(queueItemId: String, newIndex: Int): PlayerResult<Unit> = success()

    override suspend fun clearQueue(): PlayerResult<Unit> = success()

    override suspend fun play(): PlayerResult<Unit> = success()

    override suspend fun pause(): PlayerResult<Unit> = success()

    override suspend fun seekTo(positionMs: Long): PlayerResult<Unit> = success()

    override suspend fun seekToQueueItem(queueItemId: String, positionMs: Long): PlayerResult<Unit> = success()

    override suspend fun skipToNext(): PlayerResult<Unit> = success()

    override suspend fun skipToPrevious(): PlayerResult<Unit> = success()

    override suspend fun setRepeatMode(mode: RepeatMode): PlayerResult<Unit> = success()

    override suspend fun setShuffleEnabled(enabled: Boolean): PlayerResult<Unit> = success()

    override suspend fun setPlaybackSpeed(speed: Float): PlayerResult<Unit> = success()

    override suspend fun setSleepTimer(durationMs: Long?): PlayerResult<Unit> = success()

    private fun success(): PlayerResult<Unit> = PlayerResult.Success(Unit)
}

private fun track(id: String): CatalogTrackUi = CatalogTrackUi(
    id = id,
    title = "Track $id",
    artists = listOf(CatalogArtistLinkUi("artist-1", "Artist")),
    album = CatalogAlbumLinkUi("album-1", "Album"),
    artwork = null,
    durationMs = 180_000,
    discNumber = 1,
    trackNumber = 1,
)
