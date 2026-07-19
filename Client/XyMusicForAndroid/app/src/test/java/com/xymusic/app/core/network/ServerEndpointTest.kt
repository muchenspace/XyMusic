package com.xymusic.app.core.network

import com.google.common.truth.Truth.assertThat
import org.junit.Test

class ServerEndpointTest {
    @Test
    fun defaultProtocolIsHttps() {
        val endpoint = ServerEndpoint.parse("music.example", "443")

        assertThat(endpoint?.protocol).isEqualTo(ServerProtocol.HTTPS)
        assertThat(endpoint?.displayValue).isEqualTo("https://music.example")
    }

    @Test
    fun explicitHttpRemainsSupported() {
        val endpoint =
            ServerEndpoint.parse(
                host = "music.home",
                port = "3000",
                protocol = ServerProtocol.HTTP,
            )

        assertThat(endpoint?.displayValue).isEqualTo("http://music.home:3000")
    }

    @Test
    fun validInputCreatesNormalizedBaseUrl() {
        val endpoint =
            ServerEndpoint.parse(
                host = " 192.168.1.20 ",
                port = "8443",
                protocol = ServerProtocol.HTTPS,
            )

        assertThat(endpoint).isNotNull()
        assertThat(endpoint?.baseUrl.toString()).isEqualTo("https://192.168.1.20:8443/")
        assertThat(endpoint?.displayValue).isEqualTo("https://192.168.1.20:8443")
    }

    @Test
    fun invalidHostOrPortIsRejected() {
        assertThat(ServerEndpoint.parse("http://192.168.1.20", "3000")).isNull()
        assertThat(ServerEndpoint.parse("192.168.1.20/path", "3000")).isNull()
        assertThat(ServerEndpoint.parse("192.168.1.20", "0")).isNull()
        assertThat(ServerEndpoint.parse("192.168.1.20", "65536")).isNull()
    }
}
