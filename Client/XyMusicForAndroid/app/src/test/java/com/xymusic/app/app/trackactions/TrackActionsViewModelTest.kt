package com.xymusic.app.app.trackactions

import androidx.paging.PagingData
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.core.paging.asPagedStream
import com.xymusic.app.domain.paging.PagedStream
import com.xymusic.app.feature.library.domain.LibraryRepository
import com.xymusic.app.feature.library.domain.LibraryResult
import com.xymusic.app.feature.library.domain.LibraryUseCases
import com.xymusic.app.feature.library.domain.model.FavoriteSort
import com.xymusic.app.feature.library.domain.model.PlaybackHistoryItem
import com.xymusic.app.feature.library.domain.model.PlaybackProgressCommand
import com.xymusic.app.feature.player.domain.OfflineTrack
import com.xymusic.app.feature.player.domain.OfflineTrackRepository
import com.xymusic.app.feature.player.domain.OfflineTrackResult
import com.xymusic.app.feature.player.domain.OfflineTrackUseCases
import com.xymusic.app.feature.playlist.domain.PlaylistRepository
import com.xymusic.app.feature.playlist.domain.PlaylistResult
import com.xymusic.app.feature.playlist.domain.PlaylistUseCases
import com.xymusic.app.feature.playlist.domain.model.AddPlaylistTrackCommand
import com.xymusic.app.feature.playlist.domain.model.CreatePlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.PlaylistDetail
import com.xymusic.app.feature.playlist.domain.model.PlaylistSort
import com.xymusic.app.feature.playlist.domain.model.PlaylistSummary
import com.xymusic.app.feature.playlist.domain.model.RemovePlaylistTrackCommand
import com.xymusic.app.feature.playlist.domain.model.ReorderPlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.UpdatePlaylistCommand
import com.xymusic.app.support.MainDispatcherRule
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Rule
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class TrackActionsViewModelTest {
    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    @Test
    fun rapidFavoriteTogglesAreQueuedAndLatestIntentWins() = runTest {
        val library = FakeLibraryRepository().apply { blockFirstCall = true }
        val viewModel = viewModel(library)
        backgroundScope.launch { viewModel.uiState.collect() }

        viewModel.setPlayerTrack(TRACK_ID)
        runCurrent()
        viewModel.togglePlayerFavorite()
        runCurrent()
        library.firstCallStarted.await()

        assertThat(viewModel.uiState.value.playerIsFavorite).isTrue()

        viewModel.togglePlayerFavorite()
        runCurrent()

        assertThat(viewModel.uiState.value.playerIsFavorite).isFalse()
        assertThat(library.calls).containsExactly(TRACK_ID to true)

        library.releaseFirstCall.complete(Unit)
        advanceUntilIdle()

        assertThat(library.calls)
            .containsExactly(TRACK_ID to true, TRACK_ID to false)
            .inOrder()
        assertThat(library.isFavorite(TRACK_ID)).isFalse()
        assertThat(viewModel.uiState.value.playerIsFavorite).isFalse()
        assertThat(viewModel.uiState.value.isFavoriteMutating).isFalse()
    }

    private fun viewModel(library: FakeLibraryRepository) = TrackActionsViewModel(
        libraryUseCases = LibraryUseCases(library),
        playlistUseCases = PlaylistUseCases(FakePlaylistRepository),
        offlineTrackUseCases = OfflineTrackUseCases(FakeOfflineTrackRepository),
    )

    private companion object {
        const val TRACK_ID = "track-1"
    }
}

private class FakeLibraryRepository : LibraryRepository {
    private val favoriteStates = mutableMapOf<String, MutableStateFlow<Boolean>>()
    val calls = mutableListOf<Pair<String, Boolean>>()
    val firstCallStarted = CompletableDeferred<Unit>()
    val releaseFirstCall = CompletableDeferred<Unit>()
    var blockFirstCall = false

    override fun observeIsFavorite(trackId: String): Flow<Boolean> = stateFor(trackId)

    override fun favoriteTracks(): PagedStream<Track> = flowOf(PagingData.empty<Track>()).asPagedStream()

    override fun playbackHistory(): PagedStream<PlaybackHistoryItem> =
        flowOf(PagingData.empty<PlaybackHistoryItem>()).asPagedStream()

    override suspend fun refreshFavorites(sort: FavoriteSort): LibraryResult<Unit> = LibraryResult.Success(Unit)

    override suspend fun refreshHistory(): LibraryResult<Unit> = LibraryResult.Success(Unit)

    override suspend fun setFavorite(trackId: String, favorite: Boolean): LibraryResult<Unit> {
        calls += trackId to favorite
        if (blockFirstCall && calls.size == 1) {
            firstCallStarted.complete(Unit)
            releaseFirstCall.await()
        }
        stateFor(trackId).value = favorite
        return LibraryResult.Success(Unit)
    }

    override suspend fun recordPlayback(command: PlaybackProgressCommand): LibraryResult<Unit> =
        LibraryResult.Success(Unit)

    fun isFavorite(trackId: String): Boolean = stateFor(trackId).value

    private fun stateFor(trackId: String): MutableStateFlow<Boolean> =
        favoriteStates.getOrPut(trackId) { MutableStateFlow(false) }
}

private object FakePlaylistRepository : PlaylistRepository {
    override fun observePlaylists(): Flow<List<PlaylistSummary>> = flowOf(emptyList())

    override fun observePlaylist(playlistId: String): Flow<PlaylistDetail?> = flowOf(null)

    override suspend fun refreshPlaylists(sort: PlaylistSort): PlaylistResult<Unit> = PlaylistResult.Success(Unit)

    override suspend fun refreshPlaylist(playlistId: String): PlaylistResult<Unit> = PlaylistResult.Success(Unit)

    override suspend fun create(command: CreatePlaylistCommand): PlaylistResult<PlaylistSummary> = error("Not used")

    override suspend fun update(command: UpdatePlaylistCommand): PlaylistResult<PlaylistSummary> = error("Not used")

    override suspend fun delete(playlistId: String, expectedVersion: Long): PlaylistResult<Unit> = error("Not used")

    override suspend fun addTrack(command: AddPlaylistTrackCommand): PlaylistResult<Unit> = error("Not used")

    override suspend fun removeTrack(command: RemovePlaylistTrackCommand): PlaylistResult<Unit> = error("Not used")

    override suspend fun reorder(command: ReorderPlaylistCommand): PlaylistResult<Unit> = error("Not used")
}

private object FakeOfflineTrackRepository : OfflineTrackRepository {
    override fun observeAll(): Flow<List<OfflineTrack>> = flowOf(emptyList())

    override fun observeDownloaded(trackId: String): Flow<Boolean> = flowOf(false)

    override suspend fun download(trackId: String): OfflineTrackResult = OfflineTrackResult.Success

    override suspend fun remove(trackId: String): OfflineTrackResult = OfflineTrackResult.Success
}
