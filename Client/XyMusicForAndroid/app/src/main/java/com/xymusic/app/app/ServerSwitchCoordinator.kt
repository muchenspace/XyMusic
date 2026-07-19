package com.xymusic.app.app

import com.xymusic.app.core.common.IoDispatcher
import com.xymusic.app.core.network.ServerConfigRepository
import com.xymusic.app.core.network.ServerEndpoint
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.session.SessionMutationCoordinator
import dagger.Lazy
import java.util.concurrent.CancellationException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext

@Singleton
class ServerSwitchCoordinator
@Inject
constructor(
    private val serverConfigRepository: ServerConfigRepository,
    private val serverRuntimeCoordinator: ServerRuntimeCoordinator,
    private val sessionMutationCoordinator: SessionMutationCoordinator,
    private val serverCacheCleaner: Lazy<ServerDataCleaner>,
    @IoDispatcher private val ioDispatcher: CoroutineDispatcher,
) {
    private val switchMutex = Mutex()
    private val mutableState = MutableStateFlow<ServerSwitchState>(ServerSwitchState.Idle)
    private var attemptId = 0L

    val state: StateFlow<ServerSwitchState> = mutableState.asStateFlow()

    suspend fun switchTo(endpoint: ServerEndpoint) = switchMutex.withLock {
        serverConfigRepository.load()
        val previousEndpoint = serverConfigRepository.currentEndpoint()
        if (previousEndpoint == endpoint) {
            mutableState.value = ServerSwitchState.Idle
            return@withLock
        }

        val currentAttempt = ++attemptId
        mutableState.value = ServerSwitchState.Switching(endpoint)
        try {
            sessionMutationCoordinator.mutate {
                val generation = serverRuntimeCoordinator.beginSwitch()
                try {
                    withContext(NonCancellable + ioDispatcher) {
                        if (previousEndpoint != null) {
                            serverCacheCleaner.get().clearAllServerData()
                        }
                        serverConfigRepository.update(endpoint)
                    }
                } finally {
                    serverRuntimeCoordinator.finishSwitch(generation)
                }
            }
            mutableState.value = ServerSwitchState.Idle
        } catch (failure: CancellationException) {
            mutableState.value = ServerSwitchState.Failed(currentAttempt)
            throw failure
        } catch (_: Exception) {
            mutableState.value = ServerSwitchState.Failed(currentAttempt)
        }
    }
}

sealed interface ServerSwitchState {
    data object Idle : ServerSwitchState

    data class Switching(val endpoint: ServerEndpoint) : ServerSwitchState

    data class Failed(val attemptId: Long) : ServerSwitchState
}
