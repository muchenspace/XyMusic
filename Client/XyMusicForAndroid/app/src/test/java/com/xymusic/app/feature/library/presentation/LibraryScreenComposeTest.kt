package com.xymusic.app.feature.library.presentation

import android.content.Context
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasScrollToIndexAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.ComposeContentTestRule
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.paging.PagingData
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.R
import com.xymusic.app.core.model.media.AlbumReference
import com.xymusic.app.core.model.media.ArtistReference
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.feature.library.domain.LibraryRepository
import com.xymusic.app.feature.library.domain.LibraryResult
import com.xymusic.app.feature.library.domain.LibraryUseCases
import com.xymusic.app.feature.library.domain.model.FavoriteSort
import com.xymusic.app.feature.library.domain.model.PlaybackHistoryItem
import com.xymusic.app.feature.library.domain.model.PlaybackProgressCommand
import com.xymusic.app.feature.player.domain.OfflineTrack
import com.xymusic.app.feature.player.domain.OfflineTrackRepository
import com.xymusic.app.feature.player.domain.OfflineTrackResult
import com.xymusic.app.feature.player.domain.PlayerRepository
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.PlayerUseCases
import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
import com.xymusic.app.feature.player.domain.model.PlayerState
import com.xymusic.app.feature.player.domain.model.RepeatMode
import com.xymusic.app.feature.playlist.domain.PlaylistRepository
import com.xymusic.app.feature.playlist.domain.PlaylistResult
import com.xymusic.app.feature.playlist.domain.PlaylistUseCases
import com.xymusic.app.feature.playlist.domain.model.AddPlaylistTrackCommand
import com.xymusic.app.feature.playlist.domain.model.CreatePlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.PlaylistDetail
import com.xymusic.app.feature.playlist.domain.model.PlaylistSort
import com.xymusic.app.feature.playlist.domain.model.PlaylistSummary
import com.xymusic.app.feature.playlist.domain.model.PlaylistVisibility
import com.xymusic.app.feature.playlist.domain.model.RemovePlaylistTrackCommand
import com.xymusic.app.feature.playlist.domain.model.ReorderPlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.UpdatePlaylistCommand
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMusicTheme
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ComposeTestApplication::class)
class LibraryScreenComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    private val context: Context
        get() = ApplicationProvider.getApplicationContext()

    @Test
    fun playlistInitialTabShowsPlaylistsAndRoutesSelection() {
        val playlist = playlist()
        var selectedPlaylistId: String? = null
        composeRule.setLibraryContent(
            initialTab = LibraryTab.Playlists,
            playlists = listOf(playlist),
            onPlaylistClick = { selectedPlaylistId = it },
        )

        composeRule.waitForIdle()
        composeRule.onNodeWithText("Night Drive").assertIsDisplayed().performClick()
        assertThat(selectedPlaylistId).isEqualTo(playlist.id)

        composeRule.onNodeWithContentDescription(context.getString(R.string.playlist_create)).performClick()
        composeRule.onNodeWithText(context.getString(R.string.playlist_name)).assertIsDisplayed()
    }

    @Test
    fun historyPlayActionResumesFromSavedPosition() {
        val playerRepository = RecordingPlayerRepository()
        val historyItem =
            PlaybackHistoryItem(
                track = track(),
                lastPositionMs = 42_000,
                playCount = 3,
                lastPlayedAtEpochMillis = 3_000,
                completed = false,
                updatedAtEpochMillis = 3_000,
            )
        composeRule.setLibraryContent(
            initialTab = LibraryTab.History,
            history = listOf(historyItem),
            playerRepository = playerRepository,
        )

        composeRule.waitForIdle()
        composeRule
            .onNodeWithContentDescription(
                context.getString(R.string.player_play_track, historyItem.track.title),
            ).assertIsDisplayed()
            .performClick()
        composeRule.waitUntil(timeoutMillis = 5_000) {
            playerRepository.lastStartPositionMs != null
        }

        assertThat(playerRepository.lastStartPositionMs).isEqualTo(42_000)
        assertThat(playerRepository.lastQueue.single().trackId).isEqualTo(historyItem.track.id)
    }

    @Test
    fun secondaryLibraryBackInvokesCallback() {
        var backed = false
        composeRule.setLibraryContent(
            initialTab = LibraryTab.Favorites,
            onBack = { backed = true },
        )

        composeRule.onNodeWithContentDescription(context.getString(R.string.common_back)).performClick()

        assertThat(backed).isTrue()
    }

    @Test
    fun openingHistoryRefreshesOnlyHistory() {
        val libraryRepository = FakeLibraryRepository(emptyList())
        val playlistRepository = FakePlaylistRepository(emptyList())
        val viewModel =
            LibraryViewModel(
                libraryUseCases = LibraryUseCases(libraryRepository),
                playlistUseCases = PlaylistUseCases(playlistRepository),
                playerUseCases = PlayerUseCases(RecordingPlayerRepository()),
                offlineTrackRepository = EmptyOfflineTrackRepository,
                defaultDispatcher = Dispatchers.Unconfined,
            )
        composeRule.setContent {
            XyMusicTheme(darkTheme = false) {
                LibraryScreen(
                    onTrackMore = {},
                    onPlaylistClick = {},
                    initialTab = LibraryTab.History,
                    viewModel = viewModel,
                )
            }
        }

        composeRule.waitUntil(timeoutMillis = 5_000) {
            libraryRepository.historyRefreshes == 1
        }
        assertThat(libraryRepository.favoriteRefreshes).isEqualTo(0)
        assertThat(playlistRepository.refreshes).isEqualTo(0)
    }

    @Test
    fun switchingTabsPreservesHistoryScrollPosition() {
        val history =
            (1..30).map { index ->
                PlaybackHistoryItem(
                    track = track().copy(id = "track-$index", title = "Track $index"),
                    lastPositionMs = 0,
                    playCount = 1,
                    lastPlayedAtEpochMillis = index.toLong(),
                    completed = false,
                    updatedAtEpochMillis = index.toLong(),
                )
            }
        composeRule.setLibraryContent(
            initialTab = LibraryTab.History,
            history = history,
        )

        composeRule.waitForIdle()
        composeRule.onNode(hasScrollToIndexAction()).performScrollToNode(hasText("Track 24"))
        composeRule.onNodeWithText("Track 24").assertIsDisplayed()
        composeRule.onNodeWithContentDescription(context.getString(R.string.common_back)).performClick()
        composeRule.onNodeWithText(context.getString(R.string.library_favorites)).performClick()
        composeRule.waitForIdle()
        composeRule.onNodeWithContentDescription(context.getString(R.string.common_back)).performClick()
        composeRule.onNodeWithText(context.getString(R.string.library_history)).performClick()
        composeRule.waitForIdle()

        composeRule.onNodeWithText("Track 24").assertIsDisplayed()
    }

    @Test
    @Config(qualifiers = "w740dp-h320dp-land")
    fun compactLandscapeSeparatesNavigationAndShowsFirstPlaylist() {
        val playlist = playlist()
        composeRule.setLibraryContent(
            initialTab = LibraryTab.Playlists,
            playlists = listOf(playlist),
        )

        composeRule.waitForIdle()
        val navigationBounds =
            composeRule
                .onNodeWithTag(LibraryTestTags.LandscapeNavigation)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot
        val contentBounds =
            composeRule
                .onNodeWithTag(LibraryTestTags.LandscapeContent)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot

        assertThat(navigationBounds.right).isAtMost(contentBounds.left + 1f)
        composeRule.onNodeWithTag(LibraryTestTags.tab(LibraryTab.Playlists)).assertIsDisplayed()
        composeRule.onNodeWithText(playlist.name).assertIsDisplayed()
    }

    private fun ComposeContentTestRule.setLibraryContent(
        initialTab: LibraryTab,
        playlists: List<PlaylistSummary> = emptyList(),
        history: List<PlaybackHistoryItem> = emptyList(),
        playerRepository: RecordingPlayerRepository = RecordingPlayerRepository(),
        onPlaylistClick: (String) -> Unit = {},
        onBack: (() -> Unit)? = null,
    ) {
        val viewModel =
            LibraryViewModel(
                libraryUseCases = LibraryUseCases(FakeLibraryRepository(history)),
                playlistUseCases = PlaylistUseCases(FakePlaylistRepository(playlists)),
                playerUseCases = PlayerUseCases(playerRepository),
                offlineTrackRepository = EmptyOfflineTrackRepository,
                defaultDispatcher = Dispatchers.Unconfined,
            )
        setContent {
            XyMusicTheme(darkTheme = false) {
                LibraryScreen(
                    onTrackMore = {},
                    onPlaylistClick = onPlaylistClick,
                    onBack = onBack,
                    initialTab = initialTab,
                    viewModel = viewModel,
                )
            }
        }
    }

    private fun playlist() = PlaylistSummary(
        id = "playlist-1",
        ownerUserId = "user-1",
        name = "Night Drive",
        description = "Late-night favorites",
        visibility = PlaylistVisibility.PRIVATE,
        cover = null,
        trackCount = 12,
        version = 1,
        createdAtEpochMillis = 1_000,
        updatedAtEpochMillis = 1_000,
    )

    private fun track() = Track(
        id = "track-1",
        title = "First Track",
        artists = listOf(ArtistReference("artist-1", "First Artist")),
        album = AlbumReference("album-1", "First Album"),
        artwork = null,
        durationMs = 180_000,
        trackNumber = 1,
        discNumber = 1,
        publishedAtEpochMillis = 1_000,
    )
}

private class FakeLibraryRepository(history: List<PlaybackHistoryItem>) : LibraryRepository {
    private val historyFlow = flowOf(PagingData.from(history))
    var favoriteRefreshes = 0
    var historyRefreshes = 0

    override fun observeIsFavorite(trackId: String): Flow<Boolean> = flowOf(false)

    override fun favoriteTracks(): Flow<PagingData<Track>> = flowOf(PagingData.empty())

    override fun playbackHistory(): Flow<PagingData<PlaybackHistoryItem>> = historyFlow

    override suspend fun refreshFavorites(sort: FavoriteSort): LibraryResult<Unit> {
        favoriteRefreshes += 1
        return LibraryResult.Success(Unit)
    }

    override suspend fun refreshHistory(): LibraryResult<Unit> {
        historyRefreshes += 1
        return LibraryResult.Success(Unit)
    }

    override suspend fun setFavorite(trackId: String, favorite: Boolean): LibraryResult<Unit> =
        LibraryResult.Success(Unit)

    override suspend fun recordPlayback(command: PlaybackProgressCommand): LibraryResult<Unit> =
        LibraryResult.Success(Unit)
}

private class FakePlaylistRepository(playlists: List<PlaylistSummary>) : PlaylistRepository {
    private val playlistFlow = flowOf(playlists)
    var refreshes = 0

    override fun observePlaylists(): Flow<List<PlaylistSummary>> = playlistFlow

    override fun observePlaylist(playlistId: String): Flow<PlaylistDetail?> = flowOf(null)

    override suspend fun refreshPlaylists(sort: PlaylistSort): PlaylistResult<Unit> {
        refreshes += 1
        return PlaylistResult.Success(Unit)
    }

    override suspend fun refreshPlaylist(playlistId: String): PlaylistResult<Unit> = PlaylistResult.Success(Unit)

    override suspend fun create(command: CreatePlaylistCommand): PlaylistResult<PlaylistSummary> =
        PlaylistResult.Success(
            PlaylistSummary(
                id = "created-playlist",
                ownerUserId = "user-1",
                name = command.name,
                description = command.description,
                visibility = command.visibility,
                cover = null,
                trackCount = 0,
                version = 1,
                createdAtEpochMillis = 1_000,
                updatedAtEpochMillis = 1_000,
            ),
        )

    override suspend fun update(command: UpdatePlaylistCommand): PlaylistResult<PlaylistSummary> = error("Not used")

    override suspend fun delete(playlistId: String, expectedVersion: Long): PlaylistResult<Unit> =
        PlaylistResult.Success(Unit)

    override suspend fun addTrack(command: AddPlaylistTrackCommand): PlaylistResult<Unit> = PlaylistResult.Success(Unit)

    override suspend fun removeTrack(command: RemovePlaylistTrackCommand): PlaylistResult<Unit> =
        PlaylistResult.Success(Unit)

    override suspend fun reorder(command: ReorderPlaylistCommand): PlaylistResult<Unit> = PlaylistResult.Success(Unit)
}

private class RecordingPlayerRepository : PlayerRepository {
    private val mutableState = MutableStateFlow(PlayerState())
    override val state: StateFlow<PlayerState> = mutableState
    var lastQueue: List<PlayerQueueItem> = emptyList()
    var lastStartPositionMs: Long? = null

    override suspend fun connect(): PlayerResult<Unit> = PlayerResult.Success(Unit)

    override suspend fun disconnect() = Unit

    override suspend fun setQueue(
        items: List<PlayerQueueItem>,
        startQueueItemId: String?,
        startPositionMs: Long,
        playWhenReady: Boolean,
    ): PlayerResult<Unit> {
        lastQueue = items
        lastStartPositionMs = startPositionMs
        return PlayerResult.Success(Unit)
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

private object EmptyOfflineTrackRepository : OfflineTrackRepository {
    override fun observeAll(): Flow<List<OfflineTrack>> = flowOf(emptyList())

    override fun observeDownloaded(trackId: String): Flow<Boolean> = flowOf(false)

    override suspend fun download(trackId: String): OfflineTrackResult = OfflineTrackResult.Success

    override suspend fun remove(trackId: String): OfflineTrackResult = OfflineTrackResult.Success
}
