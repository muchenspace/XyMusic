package com.xymusic.app.feature.player.presentation

import androidx.paging.PagingData
import app.cash.turbine.test
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.R
import com.xymusic.app.core.model.media.Album
import com.xymusic.app.core.model.media.Artist
import com.xymusic.app.core.model.media.ArtistReference
import com.xymusic.app.core.model.media.Lyrics
import com.xymusic.app.core.model.media.LyricsFormat
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.core.model.media.TrackDetail
import com.xymusic.app.core.preferences.AppSettings
import com.xymusic.app.core.preferences.AppSettingsRepository
import com.xymusic.app.feature.catalog.domain.CatalogRepository
import com.xymusic.app.feature.catalog.domain.CatalogResult
import com.xymusic.app.feature.catalog.domain.model.AlbumQuery
import com.xymusic.app.feature.catalog.domain.model.ArtistQuery
import com.xymusic.app.feature.catalog.domain.model.TrackQuery
import com.xymusic.app.feature.player.domain.LyricsSource
import com.xymusic.app.feature.player.domain.PlaybackQueueStore
import com.xymusic.app.feature.player.domain.PlayerEvent
import com.xymusic.app.feature.player.domain.PlayerRepository
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.PlayerUseCases
import com.xymusic.app.feature.player.domain.StoredPlaybackQueueItem
import com.xymusic.app.feature.player.domain.model.PlayerFailure
import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
import com.xymusic.app.feature.player.domain.model.PlayerState
import com.xymusic.app.feature.player.domain.model.RepeatMode
import com.xymusic.app.support.MainDispatcherRule
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Rule
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class PlayerViewModelTest {
    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    @Test
    fun currentTrackRefreshesDetailAndPublishesLyrics() = runTest {
        val item = queueItem("track-1")
        val player =
            FakePlayerRepository(
                PlayerState(queue = listOf(item), currentQueueItemId = item.queueItemId),
            )
        val catalog = RefreshingCatalogRepository()
        val viewModel =
            PlayerViewModel(
                PlayerUseCases(player),
                lyricsSource(catalog),
                EmptyPlaybackQueueStore,
                FakeAppSettingsRepository(),
                mainDispatcherRule.dispatcher,
            )
        backgroundScope.launch { viewModel.uiState.collect() }

        advanceUntilIdle()

        assertThat(catalog.refreshedTrackIds).containsExactly("track-1")
        assertThat(
            viewModel.uiState.value.lyrics
                .map(PlayerLyricLineUi::text),
        ).containsExactly("First line", "Second line")
        assertThat(viewModel.uiState.value.synchronizedLyrics).isTrue()
    }

    @Test
    fun cachedLyricsArePublishedWhileRefreshIsStillRunning() = runTest {
        val item = queueItem("track-1")
        val player =
            FakePlayerRepository(
                PlayerState(queue = listOf(item), currentQueueItemId = item.queueItemId),
            )
        val catalog = BlockingRefreshCatalogRepository("track-1")
        val viewModel =
            PlayerViewModel(
                PlayerUseCases(player),
                lyricsSource(catalog),
                EmptyPlaybackQueueStore,
                FakeAppSettingsRepository(),
                mainDispatcherRule.dispatcher,
            )
        backgroundScope.launch { viewModel.uiState.collect() }

        runCurrent()

        assertThat(catalog.refreshStarted.isCompleted).isTrue()
        assertThat(
            viewModel.uiState.value.lyrics
                .map(PlayerLyricLineUi::text),
        ).containsExactly("Cached line")
        catalog.allowRefreshToFinish.complete(Unit)
    }

    @Test
    fun positionUpdatesReuseParsedLyricsAndAdvanceCurrentLine() = runTest {
        val item = queueItem("track-1")
        val player =
            FakePlayerRepository(
                PlayerState(queue = listOf(item), currentQueueItemId = item.queueItemId),
            )
        val catalog = RefreshingCatalogRepository()
        val viewModel =
            PlayerViewModel(
                PlayerUseCases(player),
                lyricsSource(catalog),
                EmptyPlaybackQueueStore,
                FakeAppSettingsRepository(),
                mainDispatcherRule.dispatcher,
            )
        backgroundScope.launch { viewModel.uiState.collect() }
        advanceUntilIdle()
        val parsedLyrics = viewModel.uiState.value.lyrics

        player.updatePosition(15_000)
        advanceUntilIdle()

        assertThat(viewModel.uiState.value.lyrics).isSameInstanceAs(parsedLyrics)
        assertThat(viewModel.uiState.value.currentLyricIndex).isEqualTo(1)
    }

    @Test
    fun wordByWordLyricsSettingIsPublishedWithoutReparsingLyrics() = runTest {
        val item = queueItem("track-1")
        val player =
            FakePlayerRepository(
                PlayerState(queue = listOf(item), currentQueueItemId = item.queueItemId),
            )
        val settingsRepository = FakeAppSettingsRepository()
        val viewModel =
            PlayerViewModel(
                PlayerUseCases(player),
                lyricsSource(RefreshingCatalogRepository()),
                EmptyPlaybackQueueStore,
                settingsRepository,
                mainDispatcherRule.dispatcher,
            )
        backgroundScope.launch { viewModel.uiState.collect() }
        advanceUntilIdle()
        val parsedLyrics = viewModel.uiState.value.lyrics
        assertThat(viewModel.uiState.value.wordByWordLyricsEnabled).isTrue()

        settingsRepository.mutate { settings ->
            settings.copy(wordByWordLyricsEnabled = false)
        }
        advanceUntilIdle()

        assertThat(viewModel.uiState.value.wordByWordLyricsEnabled).isFalse()
        assertThat(viewModel.uiState.value.lyrics).isSameInstanceAs(parsedLyrics)
    }

    @Test
    fun playbackSpeedIsForwardedToPlayerRepository() = runTest {
        val player = FakePlayerRepository(PlayerState())
        val viewModel =
            PlayerViewModel(
                PlayerUseCases(player),
                lyricsSource(RefreshingCatalogRepository()),
                EmptyPlaybackQueueStore,
                FakeAppSettingsRepository(),
                mainDispatcherRule.dispatcher,
            )

        viewModel.setPlaybackSpeed(1.5f)
        advanceUntilIdle()

        assertThat(player.lastPlaybackSpeed).isEqualTo(1.5f)
    }

    @Test
    fun playbackModeCyclesThroughShuffleRepeatAllAndRepeatOne() = runTest {
        val player = FakePlayerRepository(PlayerState())
        val viewModel =
            PlayerViewModel(
                PlayerUseCases(player),
                lyricsSource(RefreshingCatalogRepository()),
                EmptyPlaybackQueueStore,
                FakeAppSettingsRepository(),
                mainDispatcherRule.dispatcher,
            )
        backgroundScope.launch { viewModel.uiState.collect() }
        runCurrent()

        viewModel.cyclePlaybackMode()
        advanceUntilIdle()
        assertThat(player.state.value.shuffleEnabled).isTrue()
        assertThat(player.state.value.repeatMode).isEqualTo(RepeatMode.ALL)

        viewModel.cyclePlaybackMode()
        advanceUntilIdle()
        assertThat(player.state.value.shuffleEnabled).isFalse()
        assertThat(player.state.value.repeatMode).isEqualTo(RepeatMode.ALL)

        viewModel.cyclePlaybackMode()
        advanceUntilIdle()
        assertThat(player.state.value.shuffleEnabled).isFalse()
        assertThat(player.state.value.repeatMode).isEqualTo(RepeatMode.ONE)

        viewModel.cyclePlaybackMode()
        advanceUntilIdle()
        assertThat(player.state.value.shuffleEnabled).isTrue()
        assertThat(player.state.value.repeatMode).isEqualTo(RepeatMode.ALL)
    }

    @Test
    fun sleepTimerUsesRepositoryStateAcrossViewModelRecreation() = runTest {
        val player = FakePlayerRepository(PlayerState(isPlaying = true))
        val firstViewModel =
            PlayerViewModel(
                PlayerUseCases(player),
                lyricsSource(RefreshingCatalogRepository()),
                EmptyPlaybackQueueStore,
                FakeAppSettingsRepository(),
                mainDispatcherRule.dispatcher,
            )
        backgroundScope.launch { firstViewModel.uiState.collect() }

        firstViewModel.setSleepTimer(15)
        advanceUntilIdle()

        assertThat(player.sleepTimerRequests).containsExactly(15 * 60_000L)
        assertThat(firstViewModel.uiState.value.sleepTimerRemainingMs)
            .isEqualTo(15 * 60_000L)

        val recreatedViewModel =
            PlayerViewModel(
                PlayerUseCases(player),
                lyricsSource(RefreshingCatalogRepository()),
                EmptyPlaybackQueueStore,
                FakeAppSettingsRepository(),
                mainDispatcherRule.dispatcher,
            )
        backgroundScope.launch { recreatedViewModel.uiState.collect() }
        advanceUntilIdle()

        assertThat(recreatedViewModel.uiState.value.sleepTimerRemainingMs)
            .isEqualTo(15 * 60_000L)

        recreatedViewModel.setSleepTimer(null)
        advanceUntilIdle()

        assertThat(player.sleepTimerRequests)
            .containsExactlyElementsIn(listOf(15 * 60_000L, null))
            .inOrder()
        assertThat(recreatedViewModel.uiState.value.sleepTimerRemainingMs).isNull()
    }

    @Test
    fun playbackFailuresPublishSpecificMessagesWithoutRepeatingForStateUpdates() = runTest {
        val player = FakePlayerRepository(PlayerState())
        val viewModel =
            PlayerViewModel(
                PlayerUseCases(player),
                lyricsSource(RefreshingCatalogRepository()),
                EmptyPlaybackQueueStore,
                FakeAppSettingsRepository(),
                mainDispatcherRule.dispatcher,
            )

        viewModel.effects.test {
            player.updateFailure(PlayerFailure.ConnectionUnavailable)
            advanceTimeBy(PLAYER_FAILURE_MESSAGE_DELAY_MS)
            runCurrent()
            assertThat(awaitItem())
                .isEqualTo(PlayerUiEffect.ShowMessage(R.string.player_connection_unavailable))
            player.updatePosition(1_000)
            expectNoEvents()

            player.updateFailure(null)
            runCurrent()
            player.updateFailure(PlayerFailure.InvalidQueue)
            advanceTimeBy(PLAYER_FAILURE_MESSAGE_DELAY_MS)
            runCurrent()
            assertThat(awaitItem())
                .isEqualTo(PlayerUiEffect.ShowMessage(R.string.player_invalid_queue))

            player.updateFailure(null)
            runCurrent()
            player.updateFailure(PlayerFailure.PlaybackUnavailable)
            advanceTimeBy(PLAYER_FAILURE_MESSAGE_DELAY_MS)
            runCurrent()
            assertThat(awaitItem())
                .isEqualTo(PlayerUiEffect.ShowMessage(R.string.player_playback_unavailable))

            player.updateFailure(null)
            runCurrent()
            player.updateFailure(PlayerFailure.Unexpected("ERROR_CODE_DECODING_FAILED"))
            advanceTimeBy(PLAYER_FAILURE_MESSAGE_DELAY_MS)
            runCurrent()
            assertThat(awaitItem())
                .isEqualTo(PlayerUiEffect.ShowMessage(R.string.player_playback_failed))
        }
    }

    @Test
    fun compatibleCodecFallbackPublishesDedicatedMessage() = runTest {
        val player = FakePlayerRepository(PlayerState())
        val viewModel =
            PlayerViewModel(
                PlayerUseCases(player),
                lyricsSource(RefreshingCatalogRepository()),
                EmptyPlaybackQueueStore,
                FakeAppSettingsRepository(),
                mainDispatcherRule.dispatcher,
            )

        viewModel.effects.test {
            runCurrent()
            player.emit(PlayerEvent.CompatibleCodecFallbackApplied)

            assertThat(awaitItem())
                .isEqualTo(PlayerUiEffect.ShowMessage(R.string.player_codec_fallback_applied))
        }
    }

    @Test
    fun recoveredTransientFailureDoesNotPublishAStaleErrorMessage() = runTest {
        val player = FakePlayerRepository(PlayerState())
        val viewModel =
            PlayerViewModel(
                PlayerUseCases(player),
                lyricsSource(RefreshingCatalogRepository()),
                EmptyPlaybackQueueStore,
                FakeAppSettingsRepository(),
                mainDispatcherRule.dispatcher,
            )

        viewModel.effects.test {
            player.updateFailure(PlayerFailure.Unexpected("ERROR_CODE_DECODING_FAILED"))
            runCurrent()
            player.updateFailure(null)
            advanceTimeBy(PLAYER_FAILURE_MESSAGE_DELAY_MS)
            runCurrent()

            expectNoEvents()
        }
    }

    private fun queueItem(trackId: String) = PlayerQueueItem(
        queueItemId = "queue-$trackId",
        trackId = trackId,
        title = "Track",
        artistNames = listOf("Artist"),
        albumTitle = null,
        artworkUrl = null,
        artworkCacheKey = null,
        durationMs = 60_000,
    )
}

private fun lyricsSource(repository: CatalogRepository): LyricsSource = object : LyricsSource {
    override fun observe(trackId: String): Flow<List<Lyrics>> =
        repository.observeTrack(trackId).map { detail -> detail?.lyrics.orEmpty() }

    override suspend fun refresh(trackId: String) {
        repository.refreshTrack(trackId)
    }
}

private class FakeAppSettingsRepository(initialSettings: AppSettings = AppSettings()) : AppSettingsRepository {
    private val mutableSettings = MutableStateFlow(initialSettings)
    override val settings: Flow<AppSettings> = mutableSettings

    override suspend fun update(settings: AppSettings) {
        mutableSettings.value = settings
    }

    override suspend fun mutate(transform: (AppSettings) -> AppSettings) {
        mutableSettings.value = transform(mutableSettings.value)
    }

    override suspend fun reset() {
        mutableSettings.value = AppSettings()
    }
}

private object EmptyPlaybackQueueStore : PlaybackQueueStore {
    override fun observe(): Flow<List<StoredPlaybackQueueItem>> = flowOf(emptyList())

    override suspend fun replace(ownerUserId: String, items: List<StoredPlaybackQueueItem>): PlayerResult<Unit> =
        PlayerResult.Success(Unit)

    override suspend fun updatePosition(
        ownerUserId: String,
        queueItemId: String,
        positionMs: Long,
    ): PlayerResult<Unit> = PlayerResult.Success(Unit)

    override suspend fun setCurrent(ownerUserId: String, queueItemId: String, positionMs: Long): PlayerResult<Unit> =
        PlayerResult.Success(Unit)

    override suspend fun clear(ownerUserId: String): PlayerResult<Unit> = PlayerResult.Success(Unit)
}

private class RefreshingCatalogRepository : CatalogRepository {
    private val details = mutableMapOf<String, MutableStateFlow<TrackDetail?>>()
    val refreshedTrackIds = mutableListOf<String>()

    override fun observeTrack(trackId: String): Flow<TrackDetail?> =
        details.getOrPut(trackId) { MutableStateFlow(null) }

    override suspend fun refreshTrack(trackId: String): CatalogResult<Unit> {
        refreshedTrackIds += trackId
        details.getOrPut(trackId) { MutableStateFlow(null) }.value =
            TrackDetail(
                track =
                Track(
                    id = trackId,
                    title = "Track",
                    artists = listOf(ArtistReference("artist-1", "Artist")),
                    album = null,
                    artwork = null,
                    durationMs = 60_000,
                    trackNumber = 1,
                    discNumber = 1,
                    publishedAtEpochMillis = 1,
                ),
                lyrics =
                listOf(
                    Lyrics(
                        id = "lyrics-1",
                        trackId = trackId,
                        language = "und",
                        format = LyricsFormat.LRC,
                        content = "[00:00.00]First line\n[00:10.00]Second line",
                        isDefault = true,
                        trackVersion = 1,
                        updatedAtEpochMillis = 1,
                    ),
                ),
            )
        return CatalogResult.Success(Unit)
    }

    override fun pagedTracks(query: TrackQuery): Flow<PagingData<Track>> = flowOf(PagingData.empty())

    override fun pagedArtists(query: ArtistQuery): Flow<PagingData<Artist>> = flowOf(PagingData.empty())

    override fun pagedAlbums(query: AlbumQuery): Flow<PagingData<Album>> = flowOf(PagingData.empty())

    override suspend fun randomAlbums(limit: Int): CatalogResult<List<Album>> = CatalogResult.Success(emptyList())

    override suspend fun randomTracks(limit: Int): CatalogResult<List<Track>> = CatalogResult.Success(emptyList())

    override fun observeArtist(artistId: String): Flow<Artist?> = flowOf(null)

    override fun observeAlbum(albumId: String): Flow<Album?> = flowOf(null)

    override suspend fun refreshArtist(artistId: String): CatalogResult<Unit> = CatalogResult.Success(Unit)

    override suspend fun refreshAlbum(albumId: String): CatalogResult<Unit> = CatalogResult.Success(Unit)
}

private class BlockingRefreshCatalogRepository(trackId: String) : CatalogRepository {
    val refreshStarted = CompletableDeferred<Unit>()
    val allowRefreshToFinish = CompletableDeferred<Unit>()
    private val detail =
        MutableStateFlow(
            TrackDetail(
                track =
                Track(
                    id = trackId,
                    title = "Track",
                    artists = listOf(ArtistReference("artist-1", "Artist")),
                    album = null,
                    artwork = null,
                    durationMs = 60_000,
                    trackNumber = 1,
                    discNumber = 1,
                    publishedAtEpochMillis = 1,
                ),
                lyrics =
                listOf(
                    Lyrics(
                        id = "lyrics-cached",
                        trackId = trackId,
                        language = "und",
                        format = LyricsFormat.PLAIN,
                        content = "Cached line",
                        isDefault = true,
                        trackVersion = 1,
                        updatedAtEpochMillis = 1,
                    ),
                ),
            ),
        )

    override fun observeTrack(trackId: String): Flow<TrackDetail?> = detail

    override suspend fun refreshTrack(trackId: String): CatalogResult<Unit> {
        refreshStarted.complete(Unit)
        allowRefreshToFinish.await()
        return CatalogResult.Success(Unit)
    }

    override fun pagedTracks(query: TrackQuery): Flow<PagingData<Track>> = flowOf(PagingData.empty())

    override fun pagedArtists(query: ArtistQuery): Flow<PagingData<Artist>> = flowOf(PagingData.empty())

    override fun pagedAlbums(query: AlbumQuery): Flow<PagingData<Album>> = flowOf(PagingData.empty())

    override suspend fun randomAlbums(limit: Int): CatalogResult<List<Album>> = CatalogResult.Success(emptyList())

    override suspend fun randomTracks(limit: Int): CatalogResult<List<Track>> = CatalogResult.Success(emptyList())

    override fun observeArtist(artistId: String): Flow<Artist?> = flowOf(null)

    override fun observeAlbum(albumId: String): Flow<Album?> = flowOf(null)

    override suspend fun refreshArtist(artistId: String): CatalogResult<Unit> = CatalogResult.Success(Unit)

    override suspend fun refreshAlbum(albumId: String): CatalogResult<Unit> = CatalogResult.Success(Unit)
}

private class FakePlayerRepository(initialState: PlayerState) : PlayerRepository {
    private val mutableState = MutableStateFlow(initialState)
    override val state: StateFlow<PlayerState> = mutableState
    private val mutableEvents = MutableSharedFlow<PlayerEvent>(extraBufferCapacity = 1)
    override val events: Flow<PlayerEvent> = mutableEvents
    var lastPlaybackSpeed: Float? = null
    val sleepTimerRequests = mutableListOf<Long?>()

    override suspend fun connect(): PlayerResult<Unit> = success()

    override suspend fun disconnect() = Unit

    override suspend fun setQueue(
        items: List<PlayerQueueItem>,
        startQueueItemId: String?,
        startPositionMs: Long,
        playWhenReady: Boolean,
    ): PlayerResult<Unit> = success()

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

    override suspend fun setRepeatMode(mode: RepeatMode): PlayerResult<Unit> {
        mutableState.value = mutableState.value.copy(repeatMode = mode)
        return success()
    }

    override suspend fun setShuffleEnabled(enabled: Boolean): PlayerResult<Unit> {
        mutableState.value = mutableState.value.copy(shuffleEnabled = enabled)
        return success()
    }

    override suspend fun setPlaybackSpeed(speed: Float): PlayerResult<Unit> {
        lastPlaybackSpeed = speed
        return success()
    }

    override suspend fun setSleepTimer(durationMs: Long?): PlayerResult<Unit> {
        sleepTimerRequests += durationMs
        mutableState.value = mutableState.value.copy(sleepTimerRemainingMs = durationMs)
        return success()
    }

    fun updatePosition(positionMs: Long) {
        mutableState.value = mutableState.value.copy(positionMs = positionMs)
    }

    fun updateFailure(failure: PlayerFailure?) {
        mutableState.value = mutableState.value.copy(failure = failure)
    }

    fun emit(event: PlayerEvent) {
        check(mutableEvents.tryEmit(event))
    }

    private fun success(): PlayerResult<Unit> = PlayerResult.Success(Unit)
}
