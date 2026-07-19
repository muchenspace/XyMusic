package com.xymusic.app.app.trackactions

import androidx.annotation.StringRes
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.xymusic.app.R
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.network.model.ProblemCode
import com.xymusic.app.feature.library.domain.LibraryResult
import com.xymusic.app.feature.library.domain.LibraryUseCases
import com.xymusic.app.feature.player.domain.OfflineTrackRepository
import com.xymusic.app.feature.player.domain.OfflineTrackResult
import com.xymusic.app.feature.playlist.domain.PlaylistResult
import com.xymusic.app.feature.playlist.domain.PlaylistUseCases
import com.xymusic.app.feature.playlist.domain.model.AddPlaylistTrackCommand
import com.xymusic.app.feature.playlist.domain.model.CreatePlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.PlaylistSummary
import com.xymusic.app.feature.playlist.domain.model.PlaylistVisibility
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

data class TrackActionsUiState(
    val selectedTrackId: String? = null,
    val selectedIsFavorite: Boolean = false,
    val playerTrackId: String? = null,
    val playerIsFavorite: Boolean = false,
    val playlists: List<PlaylistSummary> = emptyList(),
    val isMutating: Boolean = false,
    val selectedIsDownloaded: Boolean = false,
    val isDownloading: Boolean = false,
)

sealed interface TrackActionsUiEffect {
    data class ShowMessage(@StringRes val messageRes: Int) : TrackActionsUiEffect
}

@HiltViewModel
@OptIn(ExperimentalCoroutinesApi::class)
class TrackActionsViewModel
@Inject
constructor(
    private val libraryUseCases: LibraryUseCases,
    private val playlistUseCases: PlaylistUseCases,
    private val offlineTrackRepository: OfflineTrackRepository,
) : ViewModel() {
    private val selectedTrackId = MutableStateFlow<String?>(null)
    private val playerTrackId = MutableStateFlow<String?>(null)
    private val isMutating = MutableStateFlow(false)
    private val downloadingTrackId = MutableStateFlow<String?>(null)
    private val mutableEffects = MutableSharedFlow<TrackActionsUiEffect>(extraBufferCapacity = 1)
    val effects = mutableEffects.asSharedFlow()

    private val selectedIsFavorite = selectedTrackId.favoriteState()
    private val playerIsFavorite = playerTrackId.favoriteState()
    private val selectedIsDownloaded =
        selectedTrackId.flatMapLatest { trackId ->
            trackId?.let(offlineTrackRepository::observeDownloaded) ?: flowOf(false)
        }

    private val baseState =
        combine(
            combine(
                selectedTrackId,
                selectedIsFavorite,
                playerTrackId,
                playerIsFavorite,
                isMutating,
            ) { selected, selectedFavorite, player, playerFavorite, mutating ->
                TrackActionsUiState(
                    selectedTrackId = selected,
                    selectedIsFavorite = selectedFavorite,
                    playerTrackId = player,
                    playerIsFavorite = playerFavorite,
                    isMutating = mutating,
                )
            },
            selectedIsDownloaded,
            downloadingTrackId,
        ) { state, downloaded, downloading ->
            state.copy(
                selectedIsDownloaded = downloaded,
                isDownloading = state.selectedTrackId != null && state.selectedTrackId == downloading,
            )
        }

    val uiState =
        combine(baseState, playlistUseCases.playlists()) { state, playlists ->
            state.copy(playlists = playlists)
        }.stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000),
            initialValue = TrackActionsUiState(),
        )

    fun open(trackId: String) {
        selectedTrackId.value = trackId
        viewModelScope.launch { playlistUseCases.refreshPlaylists() }
    }

    fun dismiss() {
        selectedTrackId.value = null
    }

    fun setPlayerTrack(trackId: String?) {
        playerTrackId.value = trackId
    }

    fun toggleSelectedFavorite() {
        val trackId = selectedTrackId.value ?: return
        setFavorite(trackId, !selectedIsFavorite.value)
    }

    fun togglePlayerFavorite() {
        val trackId = playerTrackId.value ?: return
        setFavorite(trackId, !playerIsFavorite.value)
    }

    fun downloadSelected() {
        val trackId = selectedTrackId.value ?: return
        if (downloadingTrackId.value != null) return
        viewModelScope.launch {
            downloadingTrackId.value = trackId
            try {
                show(
                    when (offlineTrackRepository.download(trackId)) {
                        OfflineTrackResult.Success -> R.string.offline_download_complete
                        OfflineTrackResult.Unavailable -> R.string.offline_download_failed
                    },
                )
            } finally {
                downloadingTrackId.value = null
            }
        }
    }

    fun removeSelectedDownload() {
        val trackId = selectedTrackId.value ?: return
        mutate {
            show(
                when (offlineTrackRepository.remove(trackId)) {
                    OfflineTrackResult.Success -> R.string.offline_download_removed
                    OfflineTrackResult.Unavailable -> R.string.offline_download_remove_failed
                },
            )
        }
    }

    fun addToPlaylist(playlist: PlaylistSummary) {
        val trackId = selectedTrackId.value ?: return
        addToPlaylist(trackId, playlist, dismissOnSuccess = true)
    }

    fun createPlaylistAndAdd(name: String, description: String?, visibility: PlaylistVisibility) {
        val trackId = selectedTrackId.value ?: return
        mutate {
            when (val created = playlistUseCases.create(CreatePlaylistCommand(name, description, visibility))) {
                is PlaylistResult.Success -> addTrack(trackId, created.value)
                is PlaylistResult.Conflict -> show(R.string.playlist_version_conflict)
                is PlaylistResult.Failure -> show(R.string.playlist_operation_failed)
            }
        }
    }

    private fun setFavorite(trackId: String, favorite: Boolean) {
        mutate {
            when (libraryUseCases.setFavorite(trackId, favorite)) {
                is LibraryResult.Success ->
                    show(
                        if (favorite) R.string.library_favorite_added else R.string.library_favorite_removed,
                    )
                is LibraryResult.Failure -> show(R.string.library_favorite_update_failed)
            }
        }
    }

    private fun addToPlaylist(trackId: String, playlist: PlaylistSummary, dismissOnSuccess: Boolean) {
        mutate { addTrack(trackId, playlist, dismissOnSuccess) }
    }

    private suspend fun addTrack(trackId: String, playlist: PlaylistSummary, dismissOnSuccess: Boolean = true) {
        when (
            val result =
                playlistUseCases.addTrack(
                    AddPlaylistTrackCommand(
                        playlistId = playlist.id,
                        expectedVersion = playlist.version,
                        trackId = trackId,
                    ),
                )
        ) {
            is PlaylistResult.Success -> {
                show(R.string.playlist_track_added)
                if (dismissOnSuccess) dismiss()
            }
            is PlaylistResult.Conflict -> {
                show(R.string.playlist_version_conflict)
                playlistUseCases.refreshPlaylists()
            }
            is PlaylistResult.Failure -> {
                val error = result.error
                show(
                    if (error is DomainError.Conflict &&
                        error.reason == ProblemCode.TrackAlreadyInPlaylist
                    ) {
                        R.string.playlist_track_already_added
                    } else {
                        R.string.playlist_operation_failed
                    },
                )
            }
        }
    }

    private fun mutate(block: suspend () -> Unit) {
        if (isMutating.value) return
        viewModelScope.launch {
            isMutating.value = true
            try {
                block()
            } finally {
                isMutating.value = false
            }
        }
    }

    private suspend fun show(@StringRes messageRes: Int) {
        mutableEffects.emit(TrackActionsUiEffect.ShowMessage(messageRes))
    }

    private fun MutableStateFlow<String?>.favoriteState() = flatMapLatest { trackId ->
        trackId?.let(libraryUseCases::observeIsFavorite) ?: flowOf(false)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), false)
}
