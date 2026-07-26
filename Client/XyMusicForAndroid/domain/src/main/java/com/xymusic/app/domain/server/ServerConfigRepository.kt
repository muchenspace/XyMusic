package com.xymusic.app.domain.server

import kotlinx.coroutines.flow.StateFlow

interface ServerConfigRepository {
    val endpoint: StateFlow<ServerEndpoint?>

    suspend fun load() = Unit

    fun currentEndpoint(): ServerEndpoint?

    suspend fun update(endpoint: ServerEndpoint)
}
