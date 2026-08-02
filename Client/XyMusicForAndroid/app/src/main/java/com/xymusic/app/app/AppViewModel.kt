package com.xymusic.app.app

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.xymusic.app.R
import com.xymusic.app.core.common.DefaultDispatcher
import com.xymusic.app.core.common.runCatchingPreservingCancellation
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.domain.server.ServerConfigUseCases
import com.xymusic.app.domain.server.ServerEndpoint
import com.xymusic.app.domain.settings.AppSettingsUseCases
import dagger.Lazy
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.yield

@HiltViewModel
class AppViewModel
@Inject
constructor(
    private val sessionProvider: Lazy<AppSessionProvider>,
    private val appSettingsUseCases: Lazy<AppSettingsUseCases>,
    private val serverConfigUseCases: Lazy<ServerConfigUseCases>,
    private val serverSwitchCoordinator: Lazy<ServerSwitchCoordinator>,
    @DefaultDispatcher private val defaultDispatcher: CoroutineDispatcher = Dispatchers.Default,
) : ViewModel() {
    private val mutableEffects = MutableSharedFlow<AppUiEffect>(extraBufferCapacity = 1)
    val effects = mutableEffects.asSharedFlow()

    private val mutableUiState = MutableStateFlow(AppUiState())
    val uiState = mutableUiState.asStateFlow()

    init {
        viewModelScope.launch {
            // Let Compose draw the lightweight loading state before touching the application graph.
            yield()
            val dependencies = withContext(defaultDispatcher) {
                val serverConfig = serverConfigUseCases.get()
                // Do not restore a session until the persisted server context is known.
                serverConfig.load()

                StartupDependencies(
                    session = sessionProvider.get(),
                    settings = appSettingsUseCases.get(),
                    serverConfig = serverConfig,
                    serverSwitch = serverSwitchCoordinator.get(),
                )
            }

            // Keep this collector in viewModelScope so it survives session restoration and is
            // cancelled with the ViewModel instead of blocking startup on an infinite Flow.
            viewModelScope.launch(context = defaultDispatcher, start = CoroutineStart.UNDISPATCHED) {
                combine(
                    dependencies.session.sessionState,
                    dependencies.settings.settings,
                    dependencies.serverConfig.endpoint,
                    dependencies.serverSwitch.state,
                ) { sessionState, appSettings, serverEndpoint, serverSwitchState ->
                    AppUiState(
                        sessionState = sessionState,
                        dynamicColorEnabled = appSettings.dynamicColorEnabled,
                        themePreference = appSettings.theme,
                        serverEndpoint = serverEndpoint,
                        serverSwitchState = serverSwitchState,
                    )
                }.collect { state -> mutableUiState.value = state }
            }
            // Publish the loaded endpoint before restoring the session. MainActivity keeps the
            // splash visible while loading, so the eventual destination can pre-compose before
            // the session state exposes it.
            yield()
            withContext(defaultDispatcher) {
                dependencies.session.restoreSession()
                yield()
            }
        }
    }

    fun setDynamicColorEnabled(enabled: Boolean) {
        viewModelScope.launch {
            runCatchingPreservingCancellation {
                withContext(defaultDispatcher) {
                    appSettingsUseCases.get().mutate { settings ->
                        settings.copy(dynamicColorEnabled = enabled)
                    }
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
        viewModelScope.launch {
            withContext(defaultDispatcher) {
                if (serverConfigUseCases.get().currentEndpoint() == endpoint) return@withContext
                serverSwitchCoordinator.get().switchTo(endpoint)
            }
        }
    }

    private data class StartupDependencies(
        val session: AppSessionProvider,
        val settings: AppSettingsUseCases,
        val serverConfig: ServerConfigUseCases,
        val serverSwitch: ServerSwitchCoordinator,
    )
}

sealed interface AppUiEffect {
    data class ShowMessage(val messageRes: Int) : AppUiEffect
}
