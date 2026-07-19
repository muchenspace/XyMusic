package com.xymusic.app.feature.playlist.presentation

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.xymusic.app.R
import com.xymusic.app.core.common.DefaultDispatcher
import com.xymusic.app.core.common.runCatchingPreservingCancellation
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.core.ui.media.CatalogAlbumLinkUi
import com.xymusic.app.core.ui.media.CatalogArtistLinkUi
import com.xymusic.app.core.ui.media.CatalogArtworkUi
import com.xymusic.app.core.ui.media.CatalogTrackUi
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.PlayerUseCases
import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
import com.xymusic.app.feature.playlist.domain.PlaylistResult
import com.xymusic.app.feature.playlist.domain.PlaylistUseCases
import com.xymusic.app.feature.playlist.domain.model.PlaylistDetail
import com.xymusic.app.feature.playlist.domain.model.PlaylistDetailPage
import com.xymusic.app.feature.playlist.domain.model.PlaylistVisibility
import com.xymusic.app.feature.playlist.domain.model.RemovePlaylistTrackCommand
import com.xymusic.app.feature.playlist.domain.model.ReorderPlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.UpdatePlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.ValueChange
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

object PlaylistRouteArgs {
    const val PlaylistId = "playlistId"
}

data class PlaylistEntryUi(val entryId: String, val position: Int, val track: CatalogTrackUi)

data class PlaylistDetailUi(
    val id: String,
    val name: String,
    val description: String?,
    val visibility: PlaylistVisibility,
    val cover: CatalogArtworkUi? = null,
    val trackCount: Int,
    val version: Long,
    val entries: List<PlaylistEntryUi>,
)

data class PlaylistUiState(
    val detail: PlaylistDetailUi? = null,
    val isRefreshing: Boolean = false,
    val refreshFailed: Boolean = false,
    val hasMore: Boolean = false,
    val isLoadingMore: Boolean = false,
    val loadMoreFailed: Boolean = false,
    val isMutating: Boolean = false,
) {
    val entriesComplete: Boolean
        get() = detail != null && !hasMore && detail.entries.size == detail.trackCount
}

sealed interface PlaylistUiEffect {
    data class ShowMessage(val messageRes: Int) : PlaylistUiEffect

    data object Deleted : PlaylistUiEffect
}

@HiltViewModel
class PlaylistViewModel
@Inject
constructor(
    savedStateHandle: SavedStateHandle,
    private val playlistUseCases: PlaylistUseCases,
    private val playerUseCases: PlayerUseCases,
    @DefaultDispatcher private val defaultDispatcher: CoroutineDispatcher,
) : ViewModel() {
    private val playlistId = requireNotNull(savedStateHandle.get<String>(PlaylistRouteArgs.PlaylistId))
    private val pagedDetail = MutableStateFlow<PlaylistDetail?>(null)
    private val nextCursor = MutableStateFlow<String?>(null)
    private val isRefreshing = MutableStateFlow(false)
    private val refreshFailed = MutableStateFlow(false)
    private val isLoadingMore = MutableStateFlow(false)
    private val loadMoreFailed = MutableStateFlow(false)
    private val isMutating = MutableStateFlow(false)
    private val mutableEffects = MutableSharedFlow<PlaylistUiEffect>(extraBufferCapacity = 1)
    private var refreshJob: Job? = null
    private var loadMoreJob: Job? = null
    val effects = mutableEffects.asSharedFlow()

    private val loadingState =
        combine(
            isRefreshing,
            refreshFailed,
            isLoadingMore,
            loadMoreFailed,
        ) { refreshing, failed, loadingMore, appendFailed ->
            PlaylistLoadingState(refreshing, failed, loadingMore, appendFailed)
        }

    val uiState =
        combine(
            pagedDetail,
            nextCursor,
            loadingState,
            isMutating,
        ) { detail, cursor, loading, mutating ->
            PlaylistUiState(
                detail = detail?.toUi(),
                isRefreshing = loading.refreshing,
                refreshFailed = loading.refreshFailed,
                hasMore = cursor != null,
                isLoadingMore = loading.loadingMore,
                loadMoreFailed = loading.loadMoreFailed,
                isMutating = mutating,
            )
        }.stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000),
            initialValue = PlaylistUiState(),
        )

    init {
        viewModelScope.launch {
            playlistUseCases.playlist(playlistId).collectLatest { cached ->
                if (nextCursor.value == null && !isRefreshing.value && !isLoadingMore.value) {
                    pagedDetail.value = cached
                } else if (pagedDetail.value == null && cached != null) {
                    pagedDetail.value = cached
                }
            }
        }
        refresh()
    }

    fun refresh() {
        if (refreshJob?.isActive == true) return
        loadMoreJob?.cancel()
        loadMoreJob = null
        refreshJob = viewModelScope.launch {
            isRefreshing.value = true
            refreshFailed.value = false
            loadMoreFailed.value = false
            try {
                when (
                    val result =
                        runCatchingPreservingCancellation {
                            playlistUseCases.loadPlaylistPage(playlistId, cursor = null)
                        }.getOrNull()
                ) {
                    is PlaylistResult.Success -> {
                        pagedDetail.value =
                            PlaylistDetail(
                                playlist = result.value.playlist,
                                entries = result.value.entries,
                            )
                        nextCursor.value = result.value.nextCursor
                    }
                    else -> refreshFailed.value = true
                }
            } finally {
                isRefreshing.value = false
                refreshJob = null
            }
        }
    }

    fun loadMore() {
        val cursor = nextCursor.value ?: return
        if (isRefreshing.value || loadMoreJob?.isActive == true) return
        loadMoreJob = viewModelScope.launch {
            isLoadingMore.value = true
            loadMoreFailed.value = false
            try {
                when (
                    val result =
                        runCatchingPreservingCancellation {
                            playlistUseCases.loadPlaylistPage(playlistId, cursor)
                        }.getOrNull()
                ) {
                    is PlaylistResult.Success -> appendPage(result.value)
                    else -> loadMoreFailed.value = true
                }
            } finally {
                isLoadingMore.value = false
                loadMoreJob = null
            }
        }
    }

    private fun appendPage(page: PlaylistDetailPage) {
        val current = pagedDetail.value
        if (
            current == null ||
            current.playlist.id != page.playlist.id ||
            current.playlist.version != page.playlist.version
        ) {
            loadMoreFailed.value = true
            return
        }
        val existingIds = current.entries.mapTo(hashSetOf()) { entry -> entry.id }
        if (page.entries.any { entry -> !existingIds.add(entry.id) }) {
            loadMoreFailed.value = true
            return
        }
        pagedDetail.value =
            PlaylistDetail(
                playlist = page.playlist,
                entries = (current.entries + page.entries).sortedBy { entry -> entry.position },
            )
        nextCursor.value = page.nextCursor
    }

    fun playFrom(entryId: String? = null) {
        val detail = uiState.value.detail ?: return
        if (detail.entries.isEmpty()) return
        if (entryId == null && !uiState.value.entriesComplete) {
            mutableEffects.tryEmit(PlaylistUiEffect.ShowMessage(R.string.playlist_load_all_before_action))
            return
        }
        viewModelScope.launch {
            val items =
                withContext(defaultDispatcher) {
                    detail.entries.map { entry -> entry.track.toPlayerQueueItem(entry.entryId) }
                }
            val startId = entryId ?: items.first().queueItemId
            if (
                playerUseCases.setQueue(
                    items = items,
                    startQueueItemId = startId,
                    playWhenReady = true,
                ) is PlayerResult.Failure
            ) {
                mutableEffects.emit(PlaylistUiEffect.ShowMessage(R.string.player_command_failed))
            }
        }
    }

    fun update(name: String, description: String?, visibility: PlaylistVisibility) {
        val detail = uiState.value.detail ?: return
        mutate {
            playlistUseCases.update(
                UpdatePlaylistCommand(
                    playlistId = detail.id,
                    expectedVersion = detail.version,
                    name = ValueChange.Set(name.trim()),
                    description = ValueChange.Set(description?.trim()?.takeIf(String::isNotBlank)),
                    visibility = ValueChange.Set(visibility),
                ),
            )
        }
    }

    fun delete() {
        if (isMutating.value) return
        val detail = uiState.value.detail ?: return
        viewModelScope.launch {
            isMutating.value = true
            try {
                when (
                    runCatchingPreservingCancellation {
                        playlistUseCases.delete(detail.id, detail.version)
                    }.getOrNull()
                ) {
                    is PlaylistResult.Success -> mutableEffects.emit(PlaylistUiEffect.Deleted)
                    is PlaylistResult.Conflict -> handleConflict()
                    else -> showFailure()
                }
            } finally {
                isMutating.value = false
            }
        }
    }

    fun remove(entryId: String) {
        val detail = uiState.value.detail ?: return
        mutate {
            playlistUseCases.removeTrack(
                RemovePlaylistTrackCommand(
                    playlistId = detail.id,
                    entryId = entryId,
                    expectedVersion = detail.version,
                ),
            )
        }
    }

    fun reorder(orderedEntryIds: List<String>) {
        if (!uiState.value.entriesComplete) {
            mutableEffects.tryEmit(PlaylistUiEffect.ShowMessage(R.string.playlist_load_all_before_action))
            return
        }
        val detail = uiState.value.detail ?: return
        if (
            orderedEntryIds.size != detail.entries.size ||
            orderedEntryIds.toSet() != detail.entries.map(PlaylistEntryUi::entryId).toSet()
        ) {
            return
        }
        if (orderedEntryIds == detail.entries.map(PlaylistEntryUi::entryId)) return
        mutate {
            playlistUseCases.reorder(
                ReorderPlaylistCommand(
                    playlistId = detail.id,
                    expectedVersion = detail.version,
                    orderedEntryIds = orderedEntryIds,
                ),
            )
        }
    }

    private fun mutate(command: suspend () -> PlaylistResult<*>) {
        if (isMutating.value) return
        viewModelScope.launch {
            isMutating.value = true
            try {
                when (runCatchingPreservingCancellation { command() }.getOrNull()) {
                    is PlaylistResult.Success -> refresh()
                    is PlaylistResult.Conflict -> handleConflict()
                    else -> showFailure()
                }
            } finally {
                isMutating.value = false
            }
        }
    }

    private suspend fun handleConflict() {
        mutableEffects.emit(PlaylistUiEffect.ShowMessage(R.string.playlist_version_conflict))
        refresh()
    }

    private suspend fun showFailure() {
        mutableEffects.emit(PlaylistUiEffect.ShowMessage(R.string.playlist_operation_failed))
    }
}

private fun PlaylistDetail.toUi(): PlaylistDetailUi = PlaylistDetailUi(
    id = playlist.id,
    name = playlist.name,
    description = playlist.description,
    visibility = playlist.visibility,
    cover = playlist.cover?.let { CatalogArtworkUi(it.url, it.cacheKey) },
    trackCount = playlist.trackCount,
    version = playlist.version,
    entries =
    entries.sortedBy { it.position }.map { entry ->
        PlaylistEntryUi(
            entryId = entry.id,
            position = entry.position,
            track = entry.track.toCatalogUi(),
        )
    },
)

private data class PlaylistLoadingState(
    val refreshing: Boolean,
    val refreshFailed: Boolean,
    val loadingMore: Boolean,
    val loadMoreFailed: Boolean,
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

private fun CatalogTrackUi.toPlayerQueueItem(queueItemId: String): PlayerQueueItem = PlayerQueueItem(
    queueItemId = queueItemId,
    trackId = id,
    title = title,
    artistNames = artists.map(CatalogArtistLinkUi::name),
    albumTitle = album?.title,
    artworkUrl = artwork?.url,
    artworkCacheKey = artwork?.cacheKey,
    durationMs = durationMs,
)
