package com.xymusic.app.app

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.network.ServerEndpoint
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.support.InMemoryServerConfigRepository
import dagger.Lazy
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runTest
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class ServerSwitchCoordinatorTest {
    @Test
    fun initialEndpointDoesNotInstantiateServerCacheCleaner() = runTest {
        val repository = InMemoryServerConfigRepository(null)
        var cleanerRequested = false
        val coordinator =
            ServerSwitchCoordinator(
                serverConfigRepository = repository,
                serverRuntimeCoordinator = ServerRuntimeCoordinator(),
                sessionMutationCoordinator = SessionMutationCoordinator(),
                serverCacheCleaner =
                Lazy {
                    cleanerRequested = true
                    ServerDataCleaner { }
                },
                ioDispatcher = UnconfinedTestDispatcher(testScheduler),
            )

        coordinator.switchTo(endpoint("first.example"))

        assertThat(cleanerRequested).isFalse()
        assertThat(repository.currentEndpoint()).isEqualTo(endpoint("first.example"))
    }

    @Test
    fun endpointChangesOnlyAfterCleanupSucceeds() = runTest {
        val oldEndpoint = endpoint("old.example")
        val newEndpoint = endpoint("new.example")
        val repository = InMemoryServerConfigRepository(oldEndpoint)
        val events = mutableListOf<String>()
        val coordinator =
            ServerSwitchCoordinator(
                serverConfigRepository = repository,
                serverRuntimeCoordinator = ServerRuntimeCoordinator(),
                sessionMutationCoordinator = SessionMutationCoordinator(),
                serverCacheCleaner =
                Lazy {
                    ServerDataCleaner {
                        assertThat(repository.currentEndpoint()).isEqualTo(oldEndpoint)
                        events += "cleaned"
                    }
                },
                ioDispatcher = UnconfinedTestDispatcher(testScheduler),
            )

        coordinator.switchTo(newEndpoint)

        assertThat(events).containsExactly("cleaned")
        assertThat(repository.currentEndpoint()).isEqualTo(newEndpoint)
        assertThat(coordinator.state.value).isEqualTo(ServerSwitchState.Idle)
    }

    @Test
    fun cleanupFailureKeepsOldEndpointAndReopensRuntimeGate() = runTest {
        val oldEndpoint = endpoint("old.example")
        val runtime = ServerRuntimeCoordinator()
        val repository = InMemoryServerConfigRepository(oldEndpoint)
        val coordinator =
            ServerSwitchCoordinator(
                serverConfigRepository = repository,
                serverRuntimeCoordinator = runtime,
                sessionMutationCoordinator = SessionMutationCoordinator(),
                serverCacheCleaner =
                Lazy {
                    ServerDataCleaner { throw IllegalStateException("cache busy") }
                },
                ioDispatcher = UnconfinedTestDispatcher(testScheduler),
            )

        coordinator.switchTo(endpoint("new.example"))

        assertThat(coordinator.state.value).isInstanceOf(ServerSwitchState.Failed::class.java)
        assertThat(repository.currentEndpoint()).isEqualTo(oldEndpoint)
        assertThat(runtime.state.value.isSwitching).isFalse()
        assertThat(runtime.state.value.generation).isEqualTo(1L)
    }

    private fun endpoint(host: String): ServerEndpoint = checkNotNull(ServerEndpoint.parse(host, "443"))
}
