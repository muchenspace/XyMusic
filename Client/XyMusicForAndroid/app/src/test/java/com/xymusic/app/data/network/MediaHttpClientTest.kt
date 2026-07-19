package com.xymusic.app.data.network

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.app.di.NetworkModule
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Test

class MediaHttpClientTest {
    @Test
    fun mediaClientNeverSendsAuthorizationToMediaHost() {
        val server = MockWebServer()
        server.enqueue(MockResponse().setResponseCode(200).setBody("audio"))
        server.start()
        try {
            val client =
                NetworkModule.provideMediaHttpClient(
                    removeAuthorizationInterceptor = RemoveAuthorizationInterceptor(),
                )
            val request =
                Request
                    .Builder()
                    .url(server.url("/signed/audio.mp3"))
                    .header("Authorization", "Bearer must-not-leak")
                    .build()

            client.newCall(request).execute().use { response ->
                assertThat(response.code).isEqualTo(200)
            }

            assertThat(server.takeRequest().getHeader("Authorization")).isNull()
        } finally {
            server.shutdown()
        }
    }
}
