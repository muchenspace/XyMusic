package com.xymusic.app.feature.settings.presentation

import android.net.Uri
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.xymusic.app.R
import com.xymusic.app.core.common.IoDispatcher
import com.xymusic.app.core.common.runCatchingPreservingCancellation
import com.xymusic.app.core.preferences.AppSettings
import com.xymusic.app.core.preferences.MobileDataPolicy
import com.xymusic.app.core.preferences.StreamingQuality
import com.xymusic.app.core.preferences.ThemePreference
import com.xymusic.app.feature.auth.domain.AuthResult
import com.xymusic.app.feature.auth.domain.AuthUseCases
import com.xymusic.app.feature.settings.domain.AppSettingsUseCases
import com.xymusic.app.feature.settings.domain.ProfileUseCases
import com.xymusic.app.feature.settings.domain.SettingsResult
import com.xymusic.app.feature.settings.domain.model.ProfileValueChange
import com.xymusic.app.feature.settings.domain.model.UpdateProfileCommand
import com.xymusic.app.feature.settings.domain.model.UserProfile
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

data class SettingsUiState(
    val profile: UserProfile? = null,
    val settings: AppSettings = AppSettings(),
    val isRefreshingProfile: Boolean = false,
    val isSaving: Boolean = false,
)

sealed interface SettingsUiEffect {
    data class ShowMessage(val messageRes: Int) : SettingsUiEffect
}

@HiltViewModel
class SettingsViewModel
@Inject
constructor(
    private val profileUseCases: ProfileUseCases,
    private val appSettingsUseCases: AppSettingsUseCases,
    private val authUseCases: AuthUseCases,
    @IoDispatcher private val ioDispatcher: CoroutineDispatcher,
    private val avatarImageNormalizer: AvatarImageNormalizer,
) : ViewModel() {
    private val isRefreshingProfile = MutableStateFlow(false)
    private val isSaving = MutableStateFlow(false)
    private val mutableEffects = MutableSharedFlow<SettingsUiEffect>(extraBufferCapacity = 1)
    val effects = mutableEffects.asSharedFlow()

    val uiState =
        combine(
            profileUseCases.profile,
            appSettingsUseCases.settings,
            isRefreshingProfile,
            isSaving,
        ) { profile, settings, refreshing, saving ->
            SettingsUiState(profile, settings, refreshing, saving)
        }.stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000),
            initialValue = SettingsUiState(),
        )

    init {
        loadProfile(forceRefresh = false)
    }

    fun refreshProfile() {
        loadProfile(forceRefresh = true)
    }

    private fun loadProfile(forceRefresh: Boolean) {
        if (isRefreshingProfile.value) return
        viewModelScope.launch {
            isRefreshingProfile.value = true
            try {
                val result =
                    runCatchingPreservingCancellation {
                        if (forceRefresh) profileUseCases.refresh() else profileUseCases.ensureLoaded()
                    }.getOrNull()
                if (result !is SettingsResult.Success) {
                    mutableEffects.emit(
                        SettingsUiEffect.ShowMessage(R.string.settings_profile_refresh_failed),
                    )
                }
            } finally {
                isRefreshingProfile.value = false
            }
        }
    }

    fun updateProfile(displayName: String, bio: String?) {
        val profile = uiState.value.profile ?: return
        mutate {
            profileUseCases.update(
                UpdateProfileCommand(
                    expectedVersion = profile.version,
                    displayName = ProfileValueChange.Set(displayName.trim()),
                    bio = ProfileValueChange.Set(bio?.trim()?.takeIf(String::isNotBlank)),
                ),
            )
        }
    }

    fun uploadAvatar(uri: Uri) {
        if (isSaving.value) return
        viewModelScope.launch {
            isSaving.value = true
            val messageRes =
                try {
                    val command = withContext(ioDispatcher) { avatarImageNormalizer.normalize(uri) }
                    val result =
                        runCatchingPreservingCancellation {
                            profileUseCases.uploadAvatar(command)
                        }.getOrNull()
                    if (result is SettingsResult.Success) {
                        R.string.settings_profile_saved
                    } else {
                        R.string.settings_avatar_upload_failed
                    }
                } catch (_: InvalidAvatarImageException) {
                    R.string.settings_avatar_invalid_image
                } catch (_: AvatarImageTooLargeException) {
                    R.string.settings_avatar_too_large
                } finally {
                    isSaving.value = false
                }
            mutableEffects.emit(SettingsUiEffect.ShowMessage(messageRes))
        }
    }

    fun setTheme(theme: ThemePreference) = updateSettings { copy(theme = theme) }

    fun setWordByWordLyricsEnabled(enabled: Boolean) = updateSettings {
        copy(wordByWordLyricsEnabled = enabled)
    }

    fun setStreamingQuality(quality: StreamingQuality) = updateSettings { copy(streamingQuality = quality) }

    fun setWifiOnly(wifiOnly: Boolean) = updateSettings {
        copy(
            mobileDataPolicy =
            if (wifiOnly) {
                MobileDataPolicy.WIFI_ONLY
            } else {
                MobileDataPolicy.ALLOW_STREAMING
            },
        )
    }

    fun setCacheLimitMiB(limit: Int) = updateSettings {
        copy(cacheLimitMiB = limit.coerceIn(128, 4_096))
    }

    fun resetSettings() {
        viewModelScope.launch {
            runCatchingPreservingCancellation { appSettingsUseCases.reset() }
                .onSuccess {
                    mutableEffects.emit(SettingsUiEffect.ShowMessage(R.string.settings_reset_complete))
                }.onFailure {
                    mutableEffects.emit(SettingsUiEffect.ShowMessage(R.string.settings_save_failed))
                }
        }
    }

    fun logout() {
        viewModelScope.launch {
            val result = runCatchingPreservingCancellation { authUseCases.logout() }.getOrNull()
            if (result !is AuthResult.Success) {
                mutableEffects.emit(SettingsUiEffect.ShowMessage(R.string.settings_logout_failed))
            }
        }
    }

    fun logoutAllSessions() {
        if (isSaving.value) return
        viewModelScope.launch {
            isSaving.value = true
            try {
                when (
                    runCatchingPreservingCancellation {
                        profileUseCases.logoutAllSessions()
                    }.getOrNull()
                ) {
                    is SettingsResult.Success -> {
                        val logoutResult =
                            runCatchingPreservingCancellation {
                                authUseCases.logout()
                            }.getOrNull()
                        if (logoutResult !is AuthResult.Success) {
                            mutableEffects.emit(
                                SettingsUiEffect.ShowMessage(R.string.settings_logout_failed),
                            )
                        }
                    }
                    else ->
                        mutableEffects.emit(
                            SettingsUiEffect.ShowMessage(R.string.settings_logout_all_failed),
                        )
                }
            } finally {
                isSaving.value = false
            }
        }
    }

    private fun updateSettings(transform: AppSettings.() -> AppSettings) {
        viewModelScope.launch {
            runCatchingPreservingCancellation {
                appSettingsUseCases.mutate { settings -> settings.transform() }
            }.onFailure {
                mutableEffects.emit(SettingsUiEffect.ShowMessage(R.string.settings_save_failed))
            }
        }
    }

    private fun mutate(command: suspend () -> SettingsResult<*>) {
        if (isSaving.value) return
        viewModelScope.launch {
            isSaving.value = true
            try {
                val result = runCatchingPreservingCancellation { command() }.getOrNull()
                mutableEffects.emit(
                    SettingsUiEffect.ShowMessage(
                        if (result is SettingsResult.Success) {
                            R.string.settings_profile_saved
                        } else {
                            R.string.settings_save_failed
                        },
                    ),
                )
            } finally {
                isSaving.value = false
            }
        }
    }
}
