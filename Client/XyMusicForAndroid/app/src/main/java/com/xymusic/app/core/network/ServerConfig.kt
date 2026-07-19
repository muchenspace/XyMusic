package com.xymusic.app.core.network

import java.util.Locale
import kotlinx.coroutines.flow.StateFlow
import okhttp3.HttpUrl

enum class ServerProtocol(val scheme: String) {
    HTTP("http"),
    HTTPS("https"),
}

@ConsistentCopyVisibility
data class ServerEndpoint private constructor(val protocol: ServerProtocol, val host: String, val port: Int) {
    val baseUrl: HttpUrl
        get() =
            HttpUrl
                .Builder()
                .scheme(protocol.scheme)
                .host(host)
                .port(port)
                .addPathSegment("")
                .build()

    val displayValue: String
        get() = baseUrl.toString().removeSuffix("/")

    companion object {
        fun parse(host: String, port: String, protocol: ServerProtocol = ServerProtocol.HTTPS): ServerEndpoint? {
            val normalizedHost = host.trim()
            val parsedPort = port.trim().toIntOrNull()
            return parsedPort
                ?.takeIf { it in 1..65_535 && normalizedHost.isValidServerHost() }
                ?.let { validPort ->
                    canonicalHost(normalizedHost, validPort, protocol)
                        ?.let { canonicalHost -> ServerEndpoint(protocol, canonicalHost, validPort) }
                }
        }

        private fun String.isValidServerHost(): Boolean = isNotEmpty() &&
            none(Char::isWhitespace) &&
            !contains('/') &&
            !contains("://")

        private fun canonicalHost(host: String, port: Int, protocol: ServerProtocol): String? = runCatching {
            HttpUrl
                .Builder()
                .scheme(protocol.scheme)
                .host(host)
                .port(port)
                .build()
                .host
        }.getOrNull()?.lowercase(Locale.ROOT)
    }
}

interface ServerConfigRepository {
    val endpoint: StateFlow<ServerEndpoint?>

    suspend fun load() = Unit

    fun currentEndpoint(): ServerEndpoint?

    suspend fun update(endpoint: ServerEndpoint)
}
