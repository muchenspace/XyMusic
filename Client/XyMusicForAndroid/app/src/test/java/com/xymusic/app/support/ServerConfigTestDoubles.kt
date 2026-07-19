package com.xymusic.app.support

import com.xymusic.app.core.network.ServerConfigRepository
import com.xymusic.app.core.network.ServerEndpoint
import com.xymusic.app.core.network.ServerProtocol
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import okhttp3.HttpUrl

class InMemoryServerConfigRepository(initialEndpoint: ServerEndpoint?) : ServerConfigRepository {
    private val mutableEndpoint = MutableStateFlow(initialEndpoint)
    override val endpoint: StateFlow<ServerEndpoint?> = mutableEndpoint

    override fun currentEndpoint(): ServerEndpoint? = mutableEndpoint.value

    override suspend fun update(endpoint: ServerEndpoint) {
        mutableEndpoint.value = endpoint
    }

    companion object {
        fun from(url: HttpUrl): InMemoryServerConfigRepository = InMemoryServerConfigRepository(
            checkNotNull(
                ServerEndpoint.parse(
                    host = url.host,
                    port = url.port.toString(),
                    protocol = if (url.isHttps) ServerProtocol.HTTPS else ServerProtocol.HTTP,
                ),
            ),
        )
    }
}
