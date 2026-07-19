package com.xymusic.app.core.network

import java.io.IOException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * Process-local gate for work that belongs to the currently configured server.
 *
 * A generation is invalidated before server cleanup starts. Callers that fetched data under an
 * older generation must validate it immediately before updating local state.
 */
@Singleton
class ServerRuntimeCoordinator
@Inject
constructor() {
    private val lock = Any()
    private val mutableState = MutableStateFlow(ServerRuntimeState())

    val state: StateFlow<ServerRuntimeState> = mutableState.asStateFlow()

    fun captureGeneration(): ServerGeneration = synchronized(lock) {
        val current = mutableState.value
        if (current.isSwitching) throw ServerSwitchInProgressException()
        ServerGeneration(current.generation)
    }

    fun requireCurrent(generation: ServerGeneration) {
        val current = mutableState.value
        if (current.isSwitching || current.generation != generation.value) {
            throw StaleServerGenerationException()
        }
    }

    fun isCurrent(generation: ServerGeneration): Boolean {
        val current = mutableState.value
        return !current.isSwitching && current.generation == generation.value
    }

    /** Invalidates all outstanding leases before any destructive cleanup begins. */
    fun beginSwitch(): ServerGeneration = synchronized(lock) {
        check(!mutableState.value.isSwitching) { "A server switch is already in progress" }
        val next = mutableState.value.generation + 1L
        mutableState.value = ServerRuntimeState(generation = next, isSwitching = true)
        ServerGeneration(next)
    }

    fun finishSwitch(generation: ServerGeneration) = synchronized(lock) {
        val current = mutableState.value
        check(current.generation == generation.value && current.isSwitching) {
            "Server switch generation is no longer active"
        }
        mutableState.value = current.copy(isSwitching = false)
    }
}

@JvmInline
value class ServerGeneration internal constructor(internal val value: Long)

data class ServerRuntimeState(val generation: Long = 0L, val isSwitching: Boolean = false)

class ServerSwitchInProgressException : IOException("Server switch is in progress")

class StaleServerGenerationException : IOException("Response belongs to a previous server")
