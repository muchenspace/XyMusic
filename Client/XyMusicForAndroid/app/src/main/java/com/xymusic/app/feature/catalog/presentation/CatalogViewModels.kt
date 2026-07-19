package com.xymusic.app.feature.catalog.presentation

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.paging.cachedIn
import androidx.paging.map
import com.xymusic.app.core.ui.media.toUi
import com.xymusic.app.feature.catalog.domain.CatalogResult
import com.xymusic.app.feature.catalog.domain.CatalogUseCases
import com.xymusic.app.feature.catalog.domain.model.AlbumQuery
import com.xymusic.app.feature.catalog.domain.model.AlbumSort
import com.xymusic.app.feature.catalog.domain.model.TrackQuery
import com.xymusic.app.feature.catalog.domain.model.TrackSort
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

@HiltViewModel
class CatalogViewModel
@Inject
constructor(
    private val useCases: CatalogUseCases,
) : ViewModel() {
    private val mutableRandomUiState = MutableStateFlow(CatalogRandomUiState())
    private var randomAlbumsJob: Job? = null
    private var randomTracksJob: Job? = null
    private var randomAlbumsLoaded = false
    private var randomTracksLoaded = false

    val randomUiState: StateFlow<CatalogRandomUiState> = mutableRandomUiState.asStateFlow()

    init {
        requestRandomAlbums()
        requestRandomTracks()
    }

    fun retryRandomAlbums() {
        if (!mutableRandomUiState.value.featuredFailed) return
        requestRandomAlbums()
    }

    fun retryRandomTracks() {
        if (!mutableRandomUiState.value.recommendedFailed) return
        requestRandomTracks()
    }

    private fun requestRandomAlbums() {
        if (randomAlbumsLoaded || randomAlbumsJob?.isActive == true) return
        mutableRandomUiState.update {
            it.copy(
                featuredLoading = true,
                featuredFailed = false,
            )
        }
        randomAlbumsJob =
            viewModelScope.launch {
                try {
                    when (val result = useCases.randomAlbums(RANDOM_ALBUM_LIMIT)) {
                        is CatalogResult.Success -> {
                            randomAlbumsLoaded = true
                            mutableRandomUiState.update {
                                it.copy(
                                    featuredAlbums = result.value.map { album -> album.toUi() },
                                    featuredFailed = false,
                                )
                            }
                        }

                        is CatalogResult.Failure -> {
                            mutableRandomUiState.update { it.copy(featuredFailed = true) }
                        }
                    }
                } catch (cancellation: CancellationException) {
                    throw cancellation
                } catch (_: Exception) {
                    mutableRandomUiState.update { it.copy(featuredFailed = true) }
                } finally {
                    mutableRandomUiState.update { it.copy(featuredLoading = false) }
                }
            }
    }

    private fun requestRandomTracks() {
        if (randomTracksLoaded || randomTracksJob?.isActive == true) return
        mutableRandomUiState.update {
            it.copy(
                recommendedLoading = true,
                recommendedFailed = false,
            )
        }
        randomTracksJob =
            viewModelScope.launch {
                try {
                    when (val result = useCases.randomTracks(RANDOM_TRACK_LIMIT)) {
                        is CatalogResult.Success -> {
                            randomTracksLoaded = true
                            mutableRandomUiState.update {
                                it.copy(
                                    recommendedTracks = result.value.map { track -> track.toUi() },
                                    recommendedFailed = false,
                                )
                            }
                        }

                        is CatalogResult.Failure -> {
                            mutableRandomUiState.update { it.copy(recommendedFailed = true) }
                        }
                    }
                } catch (cancellation: CancellationException) {
                    throw cancellation
                } catch (_: Exception) {
                    mutableRandomUiState.update { it.copy(recommendedFailed = true) }
                } finally {
                    mutableRandomUiState.update { it.copy(recommendedLoading = false) }
                }
            }
    }

    private companion object {
        const val RANDOM_ALBUM_LIMIT = 2
        const val RANDOM_TRACK_LIMIT = 16
    }
}

@HiltViewModel
class AlbumDetailViewModel
@Inject
constructor(
    savedStateHandle: SavedStateHandle,
    private val useCases: CatalogUseCases,
) : ViewModel() {
    private val albumId: String = savedStateHandle.requiredId(CatalogRouteArgs.AlbumId)
    private val isRefreshing = MutableStateFlow(false)
    private val refreshFailed = MutableStateFlow(false)
    private var refreshJob: Job? = null

    val uiState =
        combine(
            useCases.observeAlbum(albumId),
            isRefreshing,
            refreshFailed,
        ) { album, refreshing, failed ->
            CatalogDetailUiState(
                item = album?.toDetailUi(),
                isRefreshing = refreshing,
                refreshFailed = failed,
            )
        }.stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000),
            initialValue = CatalogDetailUiState(),
        )

    val tracks =
        useCases
            .tracks(
                TrackQuery(
                    albumId = albumId,
                    sort = TrackSort.ALBUM_ORDER_ASC,
                ),
            ).map { pagingData -> pagingData.map { it.toUi() } }
            .cachedIn(viewModelScope)

    init {
        refresh()
    }

    fun refresh() {
        if (refreshJob?.isActive == true) return
        refreshJob =
            viewModelScope.launch {
                isRefreshing.value = true
                refreshFailed.value = false
                refreshFailed.value =
                    try {
                        useCases.refreshAlbum(albumId) is CatalogResult.Failure
                    } catch (cancellation: CancellationException) {
                        throw cancellation
                    } catch (_: Exception) {
                        true
                    } finally {
                        isRefreshing.value = false
                    }
            }
    }
}

@HiltViewModel
class ArtistDetailViewModel
@Inject
constructor(
    savedStateHandle: SavedStateHandle,
    private val useCases: CatalogUseCases,
) : ViewModel() {
    private val artistId: String = savedStateHandle.requiredId(CatalogRouteArgs.ArtistId)
    private val isRefreshing = MutableStateFlow(false)
    private val refreshFailed = MutableStateFlow(false)
    private var refreshJob: Job? = null

    val uiState =
        combine(
            useCases.observeArtist(artistId),
            isRefreshing,
            refreshFailed,
        ) { artist, refreshing, failed ->
            CatalogDetailUiState(
                item = artist?.toDetailUi(),
                isRefreshing = refreshing,
                refreshFailed = failed,
            )
        }.stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000),
            initialValue = CatalogDetailUiState(),
        )

    val albums =
        useCases
            .albums(
                AlbumQuery(
                    artistId = artistId,
                    sort = AlbumSort.RELEASE_DATE_DESC,
                ),
            ).map { pagingData -> pagingData.map { it.toUi() } }
            .cachedIn(viewModelScope)

    val tracks =
        useCases
            .tracks(
                TrackQuery(
                    artistId = artistId,
                    sort = TrackSort.PUBLISHED_DESC,
                ),
            ).map { pagingData -> pagingData.map { it.toUi() } }
            .cachedIn(viewModelScope)

    init {
        refresh()
    }

    fun refresh() {
        if (refreshJob?.isActive == true) return
        refreshJob =
            viewModelScope.launch {
                isRefreshing.value = true
                refreshFailed.value = false
                refreshFailed.value =
                    try {
                        useCases.refreshArtist(artistId) is CatalogResult.Failure
                    } catch (cancellation: CancellationException) {
                        throw cancellation
                    } catch (_: Exception) {
                        true
                    } finally {
                        isRefreshing.value = false
                    }
            }
    }
}

private fun SavedStateHandle.requiredId(key: String): String =
    requireNotNull(get<String>(key)?.takeIf(String::isNotBlank)) {
        "Missing catalog destination argument: $key"
    }
