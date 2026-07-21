package com.xymusic.app.data.network

import com.google.common.truth.Truth.assertThat
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Test

class ServerResourceUrlInterceptorTest {
    @Test
    fun relativeObjectStorageUrlsUseTheApiRequestOrigin() {
        val server = MockWebServer()
        server.enqueue(
            MockResponse()
                .setHeader("Content-Type", "application/json")
                .setBody(
                    """
                    {
                      "artwork":{"url":"/api/v1/oss/b2JqZWN0cw/cover.jpg?X-Amz-Signature=a%2Bb"},
                      "playback":{"url":"/api/v1/oss/b2JqZWN0cw/song.flac"},
                      "external":{"url":"https://cdn.example/image.jpg"}
                    }
                    """.trimIndent(),
                ),
        )
        server.start()
        try {
            val client =
                OkHttpClient
                    .Builder()
                    .addInterceptor(ServerResourceUrlInterceptor())
                    .build()
            val response =
                client
                    .newCall(Request.Builder().url(server.url("/api/v1/tracks")).build())
                    .execute()
            response.use {
                val content = checkNotNull(it.body).string()
                val origin = server.url("/").toString().removeSuffix("/")
                assertThat(it.header("Content-Length")).isNull()
                assertThat(content).contains("$origin/api/v1/oss/b2JqZWN0cw/cover.jpg?X-Amz-Signature=a%2Bb")
                assertThat(content).contains("$origin/api/v1/oss/b2JqZWN0cw/song.flac")
                assertThat(content).contains("https://cdn.example/image.jpg")
                assertThat(content).doesNotContain("\"/api/v1/oss/")
            }
        } finally {
            server.shutdown()
        }
    }

    @Test
    fun nonJsonResponsesAreNotModified() {
        val server = MockWebServer()
        val original = "/api/v1/oss/b2JqZWN0cw/song.flac"
        server.enqueue(MockResponse().setHeader("Content-Type", "audio/flac").setBody(original))
        server.start()
        try {
            val client =
                OkHttpClient
                    .Builder()
                    .addInterceptor(ServerResourceUrlInterceptor())
                    .build()
            client
                .newCall(Request.Builder().url(server.url("/audio")).build())
                .execute()
                .use { response -> assertThat(checkNotNull(response.body).string()).isEqualTo(original) }
        } finally {
            server.shutdown()
        }
    }
}
