package com.xymusic.app.app

import app.cash.turbine.test
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.R
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.preferences.AppSettings
import com.xymusic.app.core.preferences.AppSettingsRepository
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.feature.settings.domain.AppSettingsUseCases
import com.xymusic.app.support.InMemoryServerConfigRepository
import dagger.Lazy
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class AppViewModelTest {
    @Test
    fun dynamicColorFailureShowsFailureEffectAndDoesNotPersistSuccess() = runTest {
        val settings = FailingSettingsRepository()
        val repository = InMemoryServerConfigRepository(null)
        val viewModel = AppViewModel(
            sessionProvider = object : AppSessionProvider {
                override val sessionState = MutableStateFlow<AppSessionState>(AppSessionState.SignedOut)
                override suspend fun restoreSession() = Unit
            },
            appSettingsUseCases = AppSettingsUseCases(settings),
            serverConfigRepository = repository,
            serverSwitchCoordinator = ServerSwitchCoordinator(
                serverConfigRepository = repository,
                serverRuntimeCoordinator = ServerRuntimeCoordinator(),
                sessionMutationCoordinator = SessionMutationCoordinator(),
                serverCacheCleaner = Lazy { ServerDataCleaner {} },
                ioDispatcher = kotlinx.coroutines.Dispatchers.Unconfined,
            ),
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
}
