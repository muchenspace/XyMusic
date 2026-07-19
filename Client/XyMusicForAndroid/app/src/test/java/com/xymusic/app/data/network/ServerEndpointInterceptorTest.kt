package com.xymusic.app.data.network

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.support.InMemoryServerConfigRepository
import java.io.IOException
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Test

class ServerEndpointInterceptorTest {
    @Test
    fun requestUsesTheLatestUserConfiguredOrigin() {
        val originalServer = MockWebServer()
        val configuredServer = MockWebServer()
        configuredServer.enqueue(MockResponse().setResponseCode(200))
        originalServer.start()
        configuredServer.start()
        try {
            val repository = InMemoryServerConfigRepository.from(configuredServer.url("/"))
            val client =
                OkHttpClient
                    .Builder()
                    .addInterceptor(ServerEndpointInterceptor(repository))
                    .build()

            client
                .newCall(Request.Builder().url(originalServer.url("/api/v1/me")).build())
                .execute()
                .close()

            assertThat(configuredServer.takeRequest().path).isEqualTo("/api/v1/me")
            assertThat(originalServer.requestCount).isEqualTo(0)
        } finally {
            originalServer.shutdown()
            configuredServer.shutdown()
        }
    }

    @Test
    fun requestIsRejectedWhileServerSwitchIsInProgress() {
        val server = MockWebServer()
        server.start()
        try {
            val runtime = ServerRuntimeCoordinator()
            runtime.beginSwitch()
            val client =
                OkHttpClient
                    .Builder()
                    .addInterceptor(
                        ServerEndpointInterceptor(
                            InMemoryServerConfigRepository.from(server.url("/")),
                            runtime,
                        ),
                    ).build()

            val failure =
                runCatching {
                    client
                        .newCall(Request.Builder().url(server.url("/api/v1/me")).build())
                        .execute()
                        .close()
                }.exceptionOrNull()

            assertThat(failure).isInstanceOf(IOException::class.java)
            assertThat(server.requestCount).isEqualTo(0)
        } finally {
            server.shutdown()
        }
    }
}
