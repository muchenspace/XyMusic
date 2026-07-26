package com.xymusic.app.domain.server

import java.net.IDN
import java.util.Locale

enum class ServerProtocol(val scheme: String, val defaultPort: Int) {
    HTTP("http", 80),
    HTTPS("https", 443),
}

@ConsistentCopyVisibility
data class ServerEndpoint private constructor(val protocol: ServerProtocol, val host: String, val port: Int) {
    val displayValue: String
        get() {
            val renderedHost = if (':' in host) "[$host]" else host
            val renderedPort = if (port == protocol.defaultPort) "" else ":$port"
            return "${protocol.scheme}://$renderedHost$renderedPort"
        }

    companion object {
        fun parse(host: String, port: String, protocol: ServerProtocol = ServerProtocol.HTTPS): ServerEndpoint? {
            val normalizedHost = host.trim()
            val parsedPort = port.trim().toIntOrNull() ?: return null
            if (parsedPort !in 1..65_535 || !normalizedHost.isValidServerHost()) return null
            val canonicalHost = canonicalHost(normalizedHost) ?: return null
            return ServerEndpoint(protocol, canonicalHost, parsedPort)
        }

        private fun String.isValidServerHost(): Boolean = isNotEmpty() &&
            none(Char::isWhitespace) &&
            !contains('/') &&
            !contains("://")

        private fun canonicalHost(host: String): String? {
            val unbracketed = host.removeSurrounding("[", "]")
            if (unbracketed.contains(':')) {
                val candidate = unbracketed.lowercase(Locale.ROOT)
                return candidate.takeIf { it.isIpv6Literal() }
            }
            val asciiHost = runCatching { IDN.toASCII(unbracketed) }.getOrNull() ?: return null
            val candidate = asciiHost.lowercase(Locale.ROOT)
            return candidate.takeIf { it.isHostNameOrIpv4() }
        }

        private fun String.isHostNameOrIpv4(): Boolean = isNotEmpty() &&
            all { it.isAsciiLetterOrDigit() || it == '-' || it == '.' || it == '_' } &&
            !startsWith('.') &&
            !contains("..")

        private fun Char.isAsciiLetterOrDigit(): Boolean = this in 'a'..'z' || this in 'A'..'Z' || this in '0'..'9'

        private fun String.isIpv6Literal(): Boolean {
            val compressedParts = split("::")
            if (compressedParts.size > 2) return false
            val hasCompression = compressedParts.size == 2
            val groups =
                compressedParts.flatMap { part ->
                    if (part.isEmpty()) emptyList() else part.split(":")
                }
            val groupCount = countIpv6Groups(groups) ?: return false
            return if (hasCompression) groupCount < 8 else groupCount == 8
        }

        private fun countIpv6Groups(groups: List<String>): Int? {
            var groupCount = 0
            groups.forEachIndexed { index, group ->
                val isLastGroup = index == groups.lastIndex
                groupCount +=
                    when {
                        isLastGroup && group.contains('.') -> if (group.isIpv4Literal()) 2 else return null
                        group.isNotEmpty() && group.length <= 4 && group.all { it in '0'..'9' || it in 'a'..'f' } -> 1
                        else -> return null
                    }
            }
            return groupCount
        }

        private fun String.isIpv4Literal(): Boolean {
            val octets = split(".")
            return octets.size == 4 &&
                octets.all { octet ->
                    octet.isNotEmpty() &&
                        octet.length <= 3 &&
                        octet.all { it in '0'..'9' } &&
                        octet.toInt() in 0..255
                }
        }
    }
}
