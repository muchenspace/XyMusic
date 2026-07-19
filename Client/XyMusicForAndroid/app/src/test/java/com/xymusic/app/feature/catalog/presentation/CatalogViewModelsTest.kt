package com.xymusic.app.feature.catalog.presentation

import androidx.lifecycle.SavedStateHandle
import androidx.paging.PagingData
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.model.media.Album
import com.xymusic.app.core.model.media.Artist
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.core.model.media.TrackDetail
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.feature.catalog.domain.CatalogRepository
import com.xymusic.app.feature.catalog.domain.CatalogResult
import com.xymusic.app.feature.catalog.domain.CatalogUseCases
import com.xymusic.app.feature.catalog.domain.model.AlbumQuery
import com.xymusic.app.feature.catalog.domain.model.ArtistQuery
import com.xymusic.app.feature.catalog.domain.model.TrackQuery
import com.xymusic.app.feature.catalog.domain.model.TrackSort
import com.xymusic.app.support.MainDispatcherRule
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Rule
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class CatalogViewModelsTest {
    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    @Test
    fun randomContentInitiallyRequestsAlbumsAndTracksOnce() = runTest {
        val albums = listOf(album("album-1"), album("album-2"))
        val tracks = List(16) { index -> track("track-$index") }
        val repository =
            RecordingCatalogRepository(
                randomAlbumResults = mutableListOf(CatalogResult.Success(albums)),
                randomTrackResults = mutableListOf(CatalogResult.Success(tracks)),
            )

        val viewModel =
            CatalogViewModel(
                useCases = CatalogUseCases(repository),
            )
        advanceUntilIdle()

        assertThat(repository.randomAlbumRequestLimits).containsExactly(2)
        assertThat(repository.randomTrackRequestLimits).containsExactly(16)
        assertThat(
            viewModel.randomUiState.value.featuredAlbums
                .map { it.id },
        ).containsExactly("album-1", "album-2")
            .inOrder()
        assertThat(
            viewModel.randomUiState.value.recommendedTracks
                .map { it.id },
        ).containsExactlyElementsIn(tracks.map { it.id })
            .inOrder()
        assertThat(viewModel.randomUiState.value.featuredLoading).isFalse()
        assertThat(viewModel.randomUiState.value.recommendedLoading).isFalse()
        assertThat(viewModel.randomUiState.value.featuredFailed).isFalse()
        assertThat(viewModel.randomUiState.value.recommendedFailed).isFalse()
    }

    @Test
    fun successfulRandomContentRemainsFixedForViewModelLifetime() = runTest {
        val repository =
            RecordingCatalogRepository(
                randomAlbumResults = mutableListOf(CatalogResult.Success(listOf(album("album-1")))),
                randomTrackResults = mutableListOf(CatalogResult.Success(listOf(track("track-1")))),
            )
        val viewModel =
            CatalogViewModel(
                useCases = CatalogUseCases(repository),
            )
        advanceUntilIdle()

        viewModel.retryRandomAlbums()
        viewModel.retryRandomTracks()
        advanceUntilIdle()

        assertThat(repository.randomAlbumRequestLimits).containsExactly(2)
        assertThat(repository.randomTrackRequestLimits).containsExactly(16)
        assertThat(
            viewModel.randomUiState.value.featuredAlbums
                .single()
                .id,
        ).isEqualTo("album-1")
        assertThat(
            viewModel.randomUiState.value.recommendedTracks
                .single()
                .id,
        ).isEqualTo("track-1")
    }

    @Test
    fun failedAlbumRequestRetriesWithoutReloadingSuccessfulTracks() = runTest {
        val tracks = listOf(track("track-1"), track("track-2"))
        val repository =
            RecordingCatalogRepository(
                randomAlbumResults =
                mutableListOf(
                    CatalogResult.Failure(DomainError.Network("offline")),
                    CatalogResult.Success(listOf(album("album-1"), album("album-2"))),
                ),
                randomTrackResults = mutableListOf(CatalogResult.Success(tracks)),
            )
        val viewModel =
            CatalogViewModel(
                useCases = CatalogUseCases(repository),
            )
        advanceUntilIdle()

        assertThat(viewModel.randomUiState.value.featuredFailed).isTrue()
        assertThat(
            viewModel.randomUiState.value.recommendedTracks
                .map { it.id },
        ).containsExactly("track-1", "track-2")
            .inOrder()

        viewModel.retryRandomAlbums()
        viewModel.retryRandomAlbums()
        viewModel.retryRandomTracks()
        advanceUntilIdle()

        assertThat(repository.randomAlbumRequestLimits).containsExactly(2, 2).inOrder()
        assertThat(repository.randomTrackRequestLimits).containsExactly(16)
        assertThat(
            viewModel.randomUiState.value.featuredAlbums
                .map { it.id },
        ).containsExactly("album-1", "album-2")
            .inOrder()
        assertThat(
            viewModel.randomUiState.value.recommendedTracks
                .map { it.id },
        ).containsExactly("track-1", "track-2")
            .inOrder()
        assertThat(viewModel.randomUiState.value.featuredFailed).isFalse()
    }

    @Test
    fun failedTrackRequestRetriesWithoutReloadingSuccessfulAlbums() = runTest {
        val albums = listOf(album("album-1"), album("album-2"))
        val repository =
            RecordingCatalogRepository(
                randomAlbumResults = mutableListOf(CatalogResult.Success(albums)),
                randomTrackResults =
                mutableListOf(
                    CatalogResult.Failure(DomainError.Network("offline")),
                    CatalogResult.Success(listOf(track("track-1"))),
                ),
            )
        val viewModel =
            CatalogViewModel(
                useCases = CatalogUseCases(repository),
            )
        advanceUntilIdle()

        assertThat(viewModel.randomUiState.value.recommendedFailed).isTrue()

        viewModel.retryRandomTracks()
        viewModel.retryRandomTracks()
        viewModel.retryRandomAlbums()
        advanceUntilIdle()

        assertThat(repository.randomAlbumRequestLimits).containsExactly(2)
        assertThat(repository.randomTrackRequestLimits).containsExactly(16, 16).inOrder()
        assertThat(
            viewModel.randomUiState.value.featuredAlbums
                .map { it.id },
        ).containsExactly("album-1", "album-2")
            .inOrder()
        assertThat(
            viewModel.randomUiState.value.recommendedTracks
                .single()
                .id,
        ).isEqualTo("track-1")
        assertThat(viewModel.randomUiState.value.recommendedFailed).isFalse()
    }

    @Test
    fun albumDetailAlwaysRequestsDiscAndTrackOrder() {
        val repository = RecordingCatalogRepository()
        val albumId = "00000000-0000-0000-0000-000000000001"

        AlbumDetailViewModel(
            savedStateHandle = SavedStateHandle(mapOf(CatalogRouteArgs.AlbumId to albumId)),
            useCases = CatalogUseCases(repository),
        )

        assertThat(repository.lastTrackQuery).isEqualTo(
            TrackQuery(
                albumId = albumId,
                sort = TrackSort.ALBUM_ORDER_ASC,
            ),
        )
    }

    @Test
    fun detailRefreshCancellationIsNotConvertedToFailureState() = runTest {
        val repository = RecordingCatalogRepository(cancelRefreshes = true)
        val useCases = CatalogUseCases(repository)
        val id = "00000000-0000-0000-0000-000000000001"
        val states =
            listOf(
                AlbumDetailViewModel(
                    savedStateHandle = SavedStateHandle(mapOf(CatalogRouteArgs.AlbumId to id)),
                    useCases = useCases,
                ).uiState,
                ArtistDetailViewModel(
                    savedStateHandle = SavedStateHandle(mapOf(CatalogRouteArgs.ArtistId to id)),
                    useCases = useCases,
                ).uiState,
            )
        states.forEach { state ->
            backgroundScope.launch { state.collect() }
        }

        advanceUntilIdle()

        states.forEach { state ->
            assertThat(state.value.isRefreshing).isFalse()
            assertThat(state.value.refreshFailed).isFalse()
        }
    }
}

private class RecordingCatalogRepository(
    private val cancelRefreshes: Boolean = false,
    private val randomAlbumResults: MutableList<CatalogResult<List<Album>>> =
        mutableListOf(
            CatalogResult.Success(emptyList()),
        ),
    private val randomTrackResults: MutableList<CatalogResult<List<Track>>> =
        mutableListOf(
            CatalogResult.Success(emptyList()),
        ),
) : CatalogRepository {
    var lastTrackQuery: TrackQuery? = null
    val randomAlbumRequestLimits = mutableListOf<Int>()
    val randomTrackRequestLimits = mutableListOf<Int>()

    override fun pagedTracks(query: TrackQuery): Flow<PagingData<Track>> {
        lastTrackQuery = query
        return flowOf(PagingData.empty())
    }

    override fun pagedArtists(query: ArtistQuery): Flow<PagingData<Artist>> = flowOf(PagingData.empty())

    override fun pagedAlbums(query: AlbumQuery): Flow<PagingData<Album>> = flowOf(PagingData.empty())

    override fun observeTrack(trackId: String): Flow<TrackDetail?> = flowOf(null)

    override fun observeArtist(artistId: String): Flow<Artist?> = flowOf(null)

    override fun observeAlbum(albumId: String): Flow<Album?> = flowOf(null)

    override suspend fun randomAlbums(limit: Int): CatalogResult<List<Album>> {
        randomAlbumRequestLimits += limit
        return randomAlbumResults.removeFirstResult("album")
    }

    override suspend fun randomTracks(limit: Int): CatalogResult<List<Track>> {
        randomTrackRequestLimits += limit
        return randomTrackResults.removeFirstResult("track")
    }

    override suspend fun refreshTrack(trackId: String): CatalogResult<Unit> {
        cancelIfRequested()
        return CatalogResult.Success(Unit)
    }

    override suspend fun refreshArtist(artistId: String): CatalogResult<Unit> {
        cancelIfRequested()
        return CatalogResult.Success(Unit)
    }

    override suspend fun refreshAlbum(albumId: String): CatalogResult<Unit> {
        cancelIfRequested()
        return CatalogResult.Success(Unit)
    }

    private fun cancelIfRequested() {
        if (cancelRefreshes) throw CancellationException("cancelled")
    }
}

private fun <T> MutableList<T>.removeFirstResult(name: String): T {
    check(isNotEmpty()) { "No $name random result configured" }
    return removeAt(0)
}

private fun album(id: String): Album = Album(
    id = id,
    title = "Album $id",
    artists = emptyList(),
    cover = null,
    releaseDateEpochMillis = null,
    trackCount = 1,
    description = null,
)

private fun track(id: String): Track = Track(
    id = id,
    title = "Track $id",
    artists = emptyList(),
    album = null,
    artwork = null,
    durationMs = 180_000,
    trackNumber = 1,
    discNumber = 1,
    publishedAtEpochMillis = 0,
)
