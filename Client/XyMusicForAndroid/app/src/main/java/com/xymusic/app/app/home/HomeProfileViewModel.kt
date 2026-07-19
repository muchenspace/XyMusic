package com.xymusic.app.app.home

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.xymusic.app.core.common.runCatchingPreservingCancellation
import com.xymusic.app.feature.settings.domain.ProfileUseCases
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

data class HomeProfileUiState(
    val avatarUrl: String? = null,
    val avatarCacheKey: String? = null,
)

@HiltViewModel
class HomeProfileViewModel
@Inject
constructor(
    private val profileUseCases: ProfileUseCases,
) : ViewModel() {
    val uiState =
        profileUseCases.profile
            .map { profile ->
                HomeProfileUiState(
                    avatarUrl = profile?.avatar?.url,
                    avatarCacheKey = profile?.avatar?.cacheKey,
                )
            }.stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000),
                initialValue = HomeProfileUiState(),
            )

    init {
        viewModelScope.launch {
            runCatchingPreservingCancellation { profileUseCases.ensureLoaded() }
        }
    }
}
