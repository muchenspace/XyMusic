package com.xymusic.app.domain.server

class ServerConfigUseCases(private val repository: ServerConfigRepository) {
    val endpoint = repository.endpoint

    suspend fun load() = repository.load()

    fun currentEndpoint() = repository.currentEndpoint()

    suspend fun update(endpoint: ServerEndpoint) = repository.update(endpoint)
}
