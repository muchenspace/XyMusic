package com.xymusic.app.feature.catalog.data.remote

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.data.media.remote.AlbumSummaryDto
import com.xymusic.app.core.data.media.remote.RandomCatalogRequestDto
import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Test
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

class CatalogRandomApiTest {
    @Test
    fun randomEndpointsPostLimitAsJsonAndDecodeItems() = runTest {
        val server = MockWebServer()
        server.enqueue(jsonResponse(TRACK_RESPONSE))
        server.enqueue(jsonResponse(ALBUM_RESPONSE))
        server.start()
        try {
            val api =
                Retrofit
                    .Builder()
                    .baseUrl(server.url("/"))
                    .addConverterFactory(
                        JSON
                            .asConverterFactory("application/json".toMediaType()),
                    ).build()
                    .create(CatalogApi::class.java)

            val tracks = api.randomTracks(RandomCatalogRequestDto(limit = 16))
            val trackRequest = server.takeRequest()
            val albums = api.randomAlbums(RandomCatalogRequestDto(limit = 2))
            val albumRequest = server.takeRequest()

            assertThat(trackRequest.method).isEqualTo("POST")
            assertThat(trackRequest.path).isEqualTo("/api/v1/tracks/random")
            assertThat(trackRequest.body.readUtf8()).isEqualTo("{\"limit\":16}")
            assertThat(tracks.body()?.items?.map(TrackSummaryDto::id)).containsExactly(TRACK_ID)
            assertThat(albumRequest.method).isEqualTo("POST")
            assertThat(albumRequest.path).isEqualTo("/api/v1/albums/random")
            assertThat(albumRequest.body.readUtf8()).isEqualTo("{\"limit\":2}")
            assertThat(albums.body()?.items?.map(AlbumSummaryDto::id)).containsExactly(ALBUM_ID)
        } finally {
            server.shutdown()
        }
    }

    private fun jsonResponse(body: String) = MockResponse()
        .setResponseCode(200)
        .setHeader("Content-Type", "application/json")
        .setBody(body)

    private companion object {
        val JSON = Json { explicitNulls = false }

        const val TRACK_ID = "11111111-1111-1111-1111-111111111111"
        const val ALBUM_ID = "22222222-2222-2222-2222-222222222222"
        const val ARTIST_ID = "33333333-3333-3333-3333-333333333333"
        const val TRACK_RESPONSE =
            """{"items":[{"id":"$TRACK_ID","title":"Track","artists":[{"id":"$ARTIST_ID","name":"Artist"}],"album":null,"artwork":null,"durationMs":180000,"trackNumber":1,"discNumber":1,"isFavorite":false,"publishedAt":"2026-07-13T00:00:00Z"}]}"""
        const val ALBUM_RESPONSE =
            """{"items":[{"id":"$ALBUM_ID","title":"Album","artists":[{"id":"$ARTIST_ID","name":"Artist"}],"cover":null,"releaseDate":"2026-07-13","trackCount":8}]}"""
    }
}
