package com.xymusic.app.feature.library.presentation

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.paging.cachedIn
import androidx.paging.map
import com.xymusic.app.R
import com.xymusic.app.core.common.DefaultDispatcher
import com.xymusic.app.core.common.runCatchingPreservingCancellation
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.core.ui.media.CatalogAlbumLinkUi
import com.xymusic.app.core.ui.media.CatalogArtistLinkUi
import com.xymusic.app.core.ui.media.CatalogArtworkUi
import com.xymusic.app.core.ui.media.CatalogTrackUi
import com.xymusic.app.feature.library.domain.LibraryResult
import com.xymusic.app.feature.library.domain.LibraryUseCases
import com.xymusic.app.feature.library.domain.model.PlaybackHistoryItem
import com.xymusic.app.feature.player.domain.OfflineTrack
import com.xymusic.app.feature.player.domain.OfflineTrackRepository
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.PlayerUseCases
import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
import com.xymusic.app.feature.playlist.domain.PlaylistResult
import com.xymusic.app.feature.playlist.domain.PlaylistUseCases
import com.xymusic.app.feature.playlist.domain.model.CreatePlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.PlaylistSummary
import com.xymusic.app.feature.playlist.domain.model.PlaylistVisibility
import com.xymusic.app.feature.playlist.domain.model.UpdatePlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.ValueChange
import dagger.hilt.android.lifecycle.HiltViewModel
import java.util.UUID
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

enum class LibraryTab { Favorites, Playlists, History, Downloads }

data class LibraryHistoryUi(
    val track: CatalogTrackUi,
    val lastPositionMs: Long,
    val playCount: Long,
    val completed: Boolean,
)

data class LibraryUiState(
    val selectedTab: LibraryTab = LibraryTab.Favorites,
    val playlists: List<PlaylistSummary> = emptyList(),
    val isRefreshing: Boolean = false,
    val refreshFailed: Boolean = false,
    val isMutating: Boolean = false,
)

sealed interface LibraryUiEffect {
    data class ShowMessage(val messageRes: Int) : LibraryUiEffect
}

@HiltViewModel
@OptIn(ExperimentalCoroutinesApi::class)
class LibraryViewModel
@Inject
constructor(
    private val libraryUseCases: LibraryUseCases,
    private val playlistUseCases: PlaylistUseCases,
    private val playerUseCases: PlayerUseCases,
    private val offlineTrackRepository: OfflineTrackRepository,
    private val savedStateHandle: SavedStateHandle = SavedStateHandle(),
    @DefaultDispatcher private val defaultDispatcher: CoroutineDispatcher = Dispatchers.Default,
) : ViewModel() {
    private val selectedTab =
        MutableStateFlow(
            savedStateHandle
                .get<String>(KEY_LIBRARY_TAB)
                ?.let { value -> LibraryTab.entries.firstOrNull { it.name == value } }
                ?: LibraryTab.Favorites,
        )
    private val isRefreshing = MutableStateFlow(false)
    private val refreshFailed = MutableStateFlow(false)
    private val isMutating = MutableStateFlow(false)
    private val mutableEffects = MutableSharedFlow<LibraryUiEffect>(extraBufferCapacity = 1)
    private val loadedTabs = mutableSetOf<LibraryTab>()
    private var refreshJob: Job? = null
    private var refreshGeneration = 0L
    val effects = mutableEffects.asSharedFlow()

    val favorites =
        libraryUseCases
            .favorites()
            .map { paging -> paging.map(Track::toCatalogUi) }
            .cachedIn(viewModelScope)

    val history =
        libraryUseCases
            .history()
            .map { paging -> paging.map(PlaybackHistoryItem::toUi) }
            .cachedIn(viewModelScope)

    val downloads =
        offlineTrackRepository
            .observeAll()
            .map { tracks -> tracks.map(OfflineTrack::toCatalogUi) }

    private val operationState =
        combine(isRefreshing, refreshFailed, isMutating) { refreshing, failed, mutating ->
            Triple(refreshing, failed, mutating)
        }

    private val playlists =
        selectedTab.flatMapLatest { tab ->
            if (tab == LibraryTab.Playlists) playlistUseCases.playlists() else flowOf(emptyList())
        }

    val uiState =
        combine(
            selectedTab,
            playlists,
            operationState,
        ) { tab, playlists, operation ->
            LibraryUiState(
                selectedTab = tab,
                playlists = playlists,
                isRefreshing = operation.first,
                refreshFailed = operation.second,
                isMutating = operation.third,
            )
        }.stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000),
            initialValue = LibraryUiState(selectedTab = selectedTab.value),
        )

    fun selectTab(tab: LibraryTab) {
        val changed = selectedTab.value != tab
        selectedTab.value = tab
        savedStateHandle[KEY_LIBRARY_TAB] = tab.name
        refreshFailed.value = false
        if (loadedTabs.add(tab)) {
            refresh(tab)
        } else if (changed) {
            cancelActiveRefresh()
        }
    }

    fun refresh() {
        refresh(selectedTab.value)
    }

    private fun refresh(tab: LibraryTab) {
        val generation = ++refreshGeneration
        refreshJob?.cancel()
        refreshJob =
            viewModelScope.launch {
                isRefreshing.value = true
                refreshFailed.value = false
                try {
                    val failed =
                        try {
                            when (tab) {
                                LibraryTab.Favorites ->
                                    libraryUseCases.refreshFavorites() is LibraryResult.Failure
                                LibraryTab.Playlists ->
                                    playlistUseCases.refreshPlaylists() is PlaylistResult.Failure
                                LibraryTab.History ->
                                    libraryUseCases.refreshHistory() is LibraryResult.Failure
                                LibraryTab.Downloads -> false
                            }
                        } catch (failure: CancellationException) {
                            throw failure
                        } catch (_: Exception) {
                            true
                        }
                    if (generation == refreshGeneration && selectedTab.value == tab) {
                        refreshFailed.value = failed
                    }
                } finally {
                    if (generation == refreshGeneration) {
                        isRefreshing.value = false
                        refreshJob = null
                    }
                }
            }
    }

    private fun cancelActiveRefresh() {
        refreshGeneration += 1
        refreshJob?.cancel()
        refreshJob = null
        isRefreshing.value = false
    }

    fun createPlaylist(name: String, description: String?, visibility: PlaylistVisibility) {
        viewModelScope.launch {
            val result =
                runCatchingPreservingCancellation {
                    playlistUseCases.create(
                        CreatePlaylistCommand(
                            name = name.trim(),
                            description = description?.trim()?.takeIf(String::isNotBlank),
                            visibility = visibility,
                        ),
                    )
                }.getOrNull()
            mutableEffects.emit(
                LibraryUiEffect.ShowMessage(
                    if (result is PlaylistResult.Success) {
                        R.string.playlist_created
                    } else {
                        R.string.playlist_operation_failed
                    },
                ),
            )
        }
    }

    fun updatePlaylist(playlist: PlaylistSummary, name: String, description: String?, visibility: PlaylistVisibility) {
        mutatePlaylist {
            playlistUseCases.update(
                UpdatePlaylistCommand(
                    playlistId = playlist.id,
                    expectedVersion = playlist.version,
                    name = ValueChange.Set(name.trim()),
                    description = ValueChange.Set(description?.trim()?.takeIf(String::isNotBlank)),
                    visibility = ValueChange.Set(visibility),
                ),
            )
        }
    }

    fun deletePlaylist(playlist: PlaylistSummary) {
        mutatePlaylist { playlistUseCases.delete(playlist.id, playlist.version) }
    }

    fun play(track: CatalogTrackUi, positionMs: Long = 0) {
        playQueue(
            tracks = listOf(track),
            startTrack = track,
            positionMs = positionMs,
        )
    }

    fun playQueue(tracks: List<CatalogTrackUi>, startTrack: CatalogTrackUi, positionMs: Long = 0L) {
        viewModelScope.launch {
            val queue =
                withContext(defaultDispatcher) {
                    val items = tracks.map(CatalogTrackUi::toPlayerQueueItem)
                    val startQueueItemId =
                        items
                            .firstOrNull { item -> item.trackId == startTrack.id }
                            ?.queueItemId
                    items to startQueueItemId
                }
            val startQueueItemId = queue.second ?: return@launch
            val result =
                playerUseCases.setQueue(
                    items = queue.first,
                    startQueueItemId = startQueueItemId,
                    startPositionMs = positionMs.coerceAtLeast(0),
                    playWhenReady = true,
                )
            if (result is PlayerResult.Failure) {
                mutableEffects.emit(LibraryUiEffect.ShowMessage(R.string.player_command_failed))
            }
        }
    }

    fun removeFavorite(trackId: String) {
        viewModelScope.launch {
            if (libraryUseCases.setFavorite(trackId, false) is LibraryResult.Failure) {
                mutableEffects.emit(LibraryUiEffect.ShowMessage(R.string.library_favorite_update_failed))
            }
        }
    }

    private fun mutatePlaylist(command: suspend () -> PlaylistResult<*>) {
        if (isMutating.value) return
        viewModelScope.launch {
            isMutating.value = true
            try {
                when (runCatchingPreservingCancellation { command() }.getOrNull()) {
                    is PlaylistResult.Success -> {
                        mutableEffects.emit(LibraryUiEffect.ShowMessage(R.string.playlist_updated))
                    }
                    is PlaylistResult.Conflict -> {
                        mutableEffects.emit(LibraryUiEffect.ShowMessage(R.string.playlist_version_conflict))
                        playlistUseCases.refreshPlaylists()
                    }
                    else ->
                        mutableEffects.emit(
                            LibraryUiEffect.ShowMessage(R.string.playlist_operation_failed),
                        )
                }
            } finally {
                isMutating.value = false
            }
        }
    }

    private companion object {
        const val KEY_LIBRARY_TAB = "libraryTab"
    }
}

private fun PlaybackHistoryItem.toUi(): LibraryHistoryUi = LibraryHistoryUi(
    track = track.toCatalogUi(),
    lastPositionMs = lastPositionMs,
    playCount = playCount,
    completed = completed,
)

private fun Track.toCatalogUi(): CatalogTrackUi = CatalogTrackUi(
    id = id,
    title = title,
    artists = artists.map { CatalogArtistLinkUi(it.id, it.name) },
    album = album?.let { CatalogAlbumLinkUi(it.id, it.title) },
    artwork = artwork?.let { CatalogArtworkUi(it.url, it.cacheKey) },
    durationMs = durationMs,
    discNumber = discNumber,
    trackNumber = trackNumber,
)

private fun CatalogTrackUi.toPlayerQueueItem(): PlayerQueueItem = PlayerQueueItem(
    queueItemId = UUID.randomUUID().toString(),
    trackId = id,
    title = title,
    artistNames = artists.map(CatalogArtistLinkUi::name),
    albumTitle = album?.title,
    artworkUrl = artwork?.url,
    artworkCacheKey = artwork?.cacheKey,
    durationMs = durationMs,
)

private fun OfflineTrack.toCatalogUi(): CatalogTrackUi = CatalogTrackUi(
    id = trackId,
    title = title,
    artists = artistNames.map { name -> CatalogArtistLinkUi(id = "", name = name) },
    album = albumTitle?.let { title -> CatalogAlbumLinkUi(id = "", title = title) },
    artwork = CatalogArtworkUi(artworkUrl, artworkCacheKey),
    durationMs = durationMs,
    discNumber = 1,
    trackNumber = null,
)
