package com.xymusic.app.app

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.xymusic.app.R
import com.xymusic.app.core.common.runCatchingPreservingCancellation
import com.xymusic.app.core.network.ServerConfigRepository
import com.xymusic.app.core.network.ServerEndpoint
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.feature.settings.domain.AppSettingsUseCases
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

@HiltViewModel
class AppViewModel
@Inject
constructor(
    private val sessionProvider: AppSessionProvider,
    private val appSettingsUseCases: AppSettingsUseCases,
    private val serverConfigRepository: ServerConfigRepository,
    private val serverSwitchCoordinator: ServerSwitchCoordinator,
) : ViewModel() {
    private val mutableEffects = MutableSharedFlow<AppUiEffect>(extraBufferCapacity = 1)
    val effects = mutableEffects.asSharedFlow()

    val uiState =
        combine(
            sessionProvider.sessionState,
            appSettingsUseCases.settings,
            serverConfigRepository.endpoint,
            serverSwitchCoordinator.state,
        ) { sessionState, settings, serverEndpoint, serverSwitchState ->
            AppUiState(
                sessionState = sessionState,
                dynamicColorEnabled = settings.dynamicColorEnabled,
                themePreference = settings.theme,
                serverEndpoint = serverEndpoint,
                serverSwitchState = serverSwitchState,
            )
        }.stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000),
            initialValue =
            AppUiState(
                serverEndpoint = serverConfigRepository.currentEndpoint(),
                serverSwitchState = serverSwitchCoordinator.state.value,
            ),
        )

    init {
        viewModelScope.launch {
            serverConfigRepository.load()
            sessionProvider.restoreSession()
        }
    }

    fun setDynamicColorEnabled(enabled: Boolean) {
        viewModelScope.launch {
            runCatchingPreservingCancellation {
                appSettingsUseCases.mutate { settings ->
                    settings.copy(dynamicColorEnabled = enabled)
                }
            }.fold(
                onSuccess = {
                    mutableEffects.emit(
                        AppUiEffect.ShowMessage(
                            if (enabled) {
                                R.string.settings_dynamic_color_enabled
                            } else {
                                R.string.settings_dynamic_color_disabled
                            },
                        ),
                    )
                },
                onFailure = {
                    mutableEffects.emit(AppUiEffect.ShowMessage(R.string.settings_save_failed))
                },
            )
        }
    }

    fun setServerEndpoint(endpoint: ServerEndpoint) {
        if (serverConfigRepository.currentEndpoint() == endpoint) return
        viewModelScope.launch {
            serverSwitchCoordinator.switchTo(endpoint)
        }
    }
}

sealed interface AppUiEffect {
    data class ShowMessage(val messageRes: Int) : AppUiEffect
}
