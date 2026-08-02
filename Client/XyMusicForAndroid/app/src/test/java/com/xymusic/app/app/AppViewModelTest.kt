package com.xymusic.app.app

import app.cash.turbine.test
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.R
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.domain.server.ServerConfigRepository
import com.xymusic.app.domain.server.ServerConfigUseCases
import com.xymusic.app.domain.server.ServerEndpoint
import com.xymusic.app.domain.server.ServerProtocol
import com.xymusic.app.domain.settings.AppSettings
import com.xymusic.app.domain.settings.AppSettingsRepository
import com.xymusic.app.domain.settings.AppSettingsUseCases
import com.xymusic.app.support.InMemoryServerConfigRepository
import com.xymusic.app.support.MainDispatcherRule
import dagger.Lazy
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Rule
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class AppViewModelTest {
    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    @Test
    fun initialUiStateDoesNotConstructApplicationDependencies() = runTest {
        var sessionCreated = false
        var settingsCreated = false
        var serverConfigCreated = false
        var switchCoordinatorCreated = false
        val dispatcher = StandardTestDispatcher(testScheduler)

        val viewModel = AppViewModel(
            sessionProvider = Lazy {
                sessionCreated = true
                object : AppSessionProvider {
                    override val sessionState = MutableStateFlow<AppSessionState>(AppSessionState.SignedOut)
                    override suspend fun restoreSession() = Unit
                }
            },
            appSettingsUseCases = Lazy {
                settingsCreated = true
                AppSettingsUseCases(FailingSettingsRepository())
            },
            serverConfigUseCases = Lazy {
                serverConfigCreated = true
                ServerConfigUseCases(InMemoryServerConfigRepository(null))
            },
            serverSwitchCoordinator = Lazy {
                switchCoordinatorCreated = true
                val repository = InMemoryServerConfigRepository(null)
                ServerSwitchCoordinator(
                    serverConfigRepository = repository,
                    serverRuntimeCoordinator = ServerRuntimeCoordinator(),
                    sessionMutationCoordinator = SessionMutationCoordinator(),
                    serverCacheCleaner = Lazy { ServerDataCleaner {} },
                    ioDispatcher = Dispatchers.Unconfined,
                )
            },
            defaultDispatcher = dispatcher,
        )

        assertThat(viewModel.uiState.value).isEqualTo(AppUiState())
        assertThat(sessionCreated).isFalse()
        assertThat(settingsCreated).isFalse()
        assertThat(serverConfigCreated).isFalse()
        assertThat(switchCoordinatorCreated).isFalse()
    }

    @Test
    fun startupDoesNotPublishSessionBeforeServerConfigurationIsLoaded() = runTest {
        val endpoint = checkNotNull(ServerEndpoint.parse("music.home", "8443", ServerProtocol.HTTPS))
        val loadStarted = CompletableDeferred<Unit>()
        val releaseLoad = CompletableDeferred<Unit>()
        val repository =
            DelayedServerConfigRepository(
                expectedEndpoint = endpoint,
                loadStarted = loadStarted,
                releaseLoad = releaseLoad,
            )
        val sessionState = MutableStateFlow<AppSessionState>(AppSessionState.Loading)
        val dispatcher = StandardTestDispatcher(testScheduler)
        val viewModel = AppViewModel(
            sessionProvider = Lazy {
                object : AppSessionProvider {
                    override val sessionState: StateFlow<AppSessionState> = sessionState.asStateFlow()

                    override suspend fun restoreSession() {
                        check(repository.loaded)
                        sessionState.value = AppSessionState.SignedIn("user")
                    }
                }
            },
            appSettingsUseCases = Lazy { AppSettingsUseCases(FailingSettingsRepository()) },
            serverConfigUseCases = Lazy { ServerConfigUseCases(repository) },
            serverSwitchCoordinator = Lazy {
                ServerSwitchCoordinator(
                    serverConfigRepository = repository,
                    serverRuntimeCoordinator = ServerRuntimeCoordinator(),
                    sessionMutationCoordinator = SessionMutationCoordinator(),
                    serverCacheCleaner = Lazy { ServerDataCleaner {} },
                    ioDispatcher = Dispatchers.Unconfined,
                )
            },
            defaultDispatcher = dispatcher,
        )

        runCurrent()
        loadStarted.await()
        assertThat(viewModel.uiState.value).isEqualTo(AppUiState())

        releaseLoad.complete(Unit)
        advanceUntilIdle()

        assertThat(viewModel.uiState.value.sessionState).isEqualTo(AppSessionState.SignedIn("user"))
        assertThat(viewModel.uiState.value.serverEndpoint).isEqualTo(endpoint)
    }

    @Test
    fun loadedEndpointIsPublishedWhileSessionIsStillRestoring() = runTest {
        val endpoint = checkNotNull(ServerEndpoint.parse("music.home", "8443", ServerProtocol.HTTPS))
        val restoreStarted = CompletableDeferred<Unit>()
        val releaseRestore = CompletableDeferred<Unit>()
        val repository = InMemoryServerConfigRepository(endpoint)
        val sessionState = MutableStateFlow<AppSessionState>(AppSessionState.Loading)
        val dispatcher = StandardTestDispatcher(testScheduler)
        val viewModel = AppViewModel(
            sessionProvider = Lazy {
                object : AppSessionProvider {
                    override val sessionState: StateFlow<AppSessionState> = sessionState.asStateFlow()

                    override suspend fun restoreSession() {
                        restoreStarted.complete(Unit)
                        releaseRestore.await()
                        sessionState.value = AppSessionState.SignedOut
                    }
                }
            },
            appSettingsUseCases = Lazy { AppSettingsUseCases(FailingSettingsRepository()) },
            serverConfigUseCases = Lazy { ServerConfigUseCases(repository) },
            serverSwitchCoordinator = Lazy {
                ServerSwitchCoordinator(
                    serverConfigRepository = repository,
                    serverRuntimeCoordinator = ServerRuntimeCoordinator(),
                    sessionMutationCoordinator = SessionMutationCoordinator(),
                    serverCacheCleaner = Lazy { ServerDataCleaner {} },
                    ioDispatcher = Dispatchers.Unconfined,
                )
            },
            defaultDispatcher = dispatcher,
        )

        runCurrent()
        restoreStarted.await()
        runCurrent()

        assertThat(viewModel.uiState.value.serverEndpoint).isEqualTo(endpoint)
        assertThat(viewModel.uiState.value.sessionState).isEqualTo(AppSessionState.Loading)

        releaseRestore.complete(Unit)
        advanceUntilIdle()

        assertThat(viewModel.uiState.value.sessionState).isEqualTo(AppSessionState.SignedOut)
    }

    @Test
    fun dynamicColorFailureShowsFailureEffectAndDoesNotPersistSuccess() = runTest {
        val settings = FailingSettingsRepository()
        val repository = InMemoryServerConfigRepository(null)
        val viewModel = AppViewModel(
            sessionProvider = Lazy {
                object : AppSessionProvider {
                    override val sessionState = MutableStateFlow<AppSessionState>(AppSessionState.SignedOut)
                    override suspend fun restoreSession() = Unit
                }
            },
            appSettingsUseCases = Lazy { AppSettingsUseCases(settings) },
            serverConfigUseCases = Lazy { ServerConfigUseCases(repository) },
            serverSwitchCoordinator = Lazy {
                ServerSwitchCoordinator(
                    serverConfigRepository = repository,
                    serverRuntimeCoordinator = ServerRuntimeCoordinator(),
                    sessionMutationCoordinator = SessionMutationCoordinator(),
                    serverCacheCleaner = Lazy { ServerDataCleaner {} },
                    ioDispatcher = kotlinx.coroutines.Dispatchers.Unconfined,
                )
            },
            defaultDispatcher = Dispatchers.Unconfined,
        )

        viewModel.effects.test {
            viewModel.setDynamicColorEnabled(true)
            advanceUntilIdle()
            assertThat(awaitItem()).isEqualTo(AppUiEffect.ShowMessage(R.string.settings_save_failed))
            assertThat(settings.state.value.dynamicColorEnabled).isFalse()
        }
    }

    private class FailingSettingsRepository : AppSettingsRepository {
        val state = MutableStateFlow(AppSettings())
        override val settings = state
        override suspend fun update(settings: AppSettings) = error("write failed")
        override suspend fun mutate(transform: (AppSettings) -> AppSettings) = error("write failed")
        override suspend fun reset() = error("write failed")
    }

    private class DelayedServerConfigRepository(
        private val expectedEndpoint: ServerEndpoint,
        private val loadStarted: CompletableDeferred<Unit>,
        private val releaseLoad: CompletableDeferred<Unit>,
    ) : ServerConfigRepository {
        private val mutableEndpoint = MutableStateFlow<ServerEndpoint?>(null)
        val loaded: Boolean
            get() = mutableEndpoint.value != null

        override val endpoint: StateFlow<ServerEndpoint?> = mutableEndpoint.asStateFlow()

        override suspend fun load() {
            loadStarted.complete(Unit)
            releaseLoad.await()
            mutableEndpoint.value = expectedEndpoint
        }

        override fun currentEndpoint(): ServerEndpoint? = mutableEndpoint.value

        override suspend fun update(endpoint: ServerEndpoint) {
            mutableEndpoint.value = endpoint
        }
    }
}
