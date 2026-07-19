package com.xymusic.app.app.mine

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.xymusic.app.R
import com.xymusic.app.core.common.runCatchingPreservingCancellation
import com.xymusic.app.feature.library.presentation.LibraryTab
import com.xymusic.app.feature.library.presentation.LibraryUiState
import com.xymusic.app.feature.playlist.domain.PlaylistResult
import com.xymusic.app.feature.playlist.domain.PlaylistUseCases
import com.xymusic.app.feature.playlist.domain.model.CreatePlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.PlaylistVisibility
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

sealed interface MineUiEffect {
    data class ShowMessage(val messageRes: Int) : MineUiEffect
}

@HiltViewModel
class MineViewModel
@Inject
constructor(private val playlistUseCases: PlaylistUseCases) : ViewModel() {
    private val isRefreshing = MutableStateFlow(false)
    private val refreshFailed = MutableStateFlow(false)
    private val mutableEffects = MutableSharedFlow<MineUiEffect>(extraBufferCapacity = 1)
    val effects = mutableEffects.asSharedFlow()

    val uiState =
        combine(
            playlistUseCases.playlists(),
            isRefreshing,
            refreshFailed,
        ) { playlists, refreshing, failed ->
            LibraryUiState(
                selectedTab = LibraryTab.Playlists,
                playlists = playlists,
                isRefreshing = refreshing,
                refreshFailed = failed,
            )
        }.stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000),
            initialValue = LibraryUiState(selectedTab = LibraryTab.Playlists),
        )

    init {
        refreshPlaylists()
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
                MineUiEffect.ShowMessage(
                    if (result is PlaylistResult.Success) {
                        R.string.playlist_created
                    } else {
                        R.string.playlist_operation_failed
                    },
                ),
            )
        }
    }

    private fun refreshPlaylists() {
        viewModelScope.launch {
            isRefreshing.value = true
            try {
                refreshFailed.value =
                    runCatchingPreservingCancellation {
                        playlistUseCases.refreshPlaylists()
                    }.getOrNull() !is PlaylistResult.Success
            } finally {
                isRefreshing.value = false
            }
        }
    }
}
