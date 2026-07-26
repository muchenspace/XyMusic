package com.xymusic.app.domain.server

import com.google.common.truth.Truth.assertThat
import org.junit.Test

class ServerEndpointTest {
    @Test
    fun defaultProtocolIsHttpsAndDefaultPortIsElided() {
        val endpoint = ServerEndpoint.parse("music.example", "443")

        assertThat(endpoint?.protocol).isEqualTo(ServerProtocol.HTTPS)
        assertThat(endpoint?.displayValue).isEqualTo("https://music.example")
    }

    @Test
    fun explicitHttpKeepsCustomPort() {
        val endpoint = ServerEndpoint.parse("music.home", "3000", ServerProtocol.HTTP)

        assertThat(endpoint?.displayValue).isEqualTo("http://music.home:3000")
    }

    @Test
    fun hostAndPortAreTrimmedAndHostIsLowercased() {
        val endpoint = ServerEndpoint.parse(" MUSIC.Example ", " 8443 ")

        assertThat(endpoint?.host).isEqualTo("music.example")
        assertThat(endpoint?.port).isEqualTo(8443)
    }

    @Test
    fun ipv4HostIsAccepted() {
        val endpoint = ServerEndpoint.parse("192.168.1.20", "8443")

        assertThat(endpoint?.displayValue).isEqualTo("https://192.168.1.20:8443")
    }

    @Test
    fun ipv6HostIsAcceptedAndBracketedInDisplayValue() {
        val bare = ServerEndpoint.parse("::1", "3000")
        val bracketed = ServerEndpoint.parse("[2001:db8::1]", "3000")

        assertThat(bare?.displayValue).isEqualTo("https://[::1]:3000")
        assertThat(bracketed?.host).isEqualTo("2001:db8::1")
    }

    @Test
    fun invalidHostOrPortIsRejected() {
        assertThat(ServerEndpoint.parse("http://192.168.1.20", "3000")).isNull()
        assertThat(ServerEndpoint.parse("192.168.1.20/path", "3000")).isNull()
        assertThat(ServerEndpoint.parse("192.168.1.20", "0")).isNull()
        assertThat(ServerEndpoint.parse("192.168.1.20", "65536")).isNull()
        assertThat(ServerEndpoint.parse("", "3000")).isNull()
        assertThat(ServerEndpoint.parse("bad host", "3000")).isNull()
        assertThat(ServerEndpoint.parse(":::", "3000")).isNull()
        assertThat(ServerEndpoint.parse("1:2:3:4:5:6:7:8:9", "3000")).isNull()
    }
}
