package com.xymusic.app.feature.catalog.data.remote

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.data.media.remote.AlbumDetailDto
import com.xymusic.app.core.data.media.remote.AlbumPageDto
import com.xymusic.app.core.data.media.remote.ArtistDetailDto
import com.xymusic.app.core.data.media.remote.ArtistPageDto
import com.xymusic.app.core.data.media.remote.CatalogProtocolException
import com.xymusic.app.core.data.media.remote.LyricsResourceDto
import com.xymusic.app.core.data.media.remote.RandomAlbumsResponseDto
import com.xymusic.app.core.data.media.remote.RandomCatalogRequestDto
import com.xymusic.app.core.data.media.remote.RandomTracksResponseDto
import com.xymusic.app.core.data.media.remote.TrackDetailDto
import com.xymusic.app.core.data.media.remote.TrackPageDto
import com.xymusic.app.core.network.ProblemMapper
import com.xymusic.app.data.network.ProblemResponseParser
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import org.junit.Test
import retrofit2.Response

class CatalogTrackLyricsContractTest {
    @Test
    fun trackRequestsOneDetailAndReturnsTheServerResource() = runTest {
        var calls = 0
        val api = FakeCatalogApi {
            calls += 1
            Response.success(detail(lyric = lyric()))
        }

        val result = dataSource(api).track(TRACK_ID)

        assertThat(calls).isEqualTo(1)
        assertThat(result.lyric?.id).isEqualTo("lyric-1")
        assertThat(result.lyric?.timing).isEqualTo("LINE")
    }

    @Test
    fun trackAllowsAnExplicitNullLyric() = runTest {
        val result = dataSource(FakeCatalogApi { Response.success(detail(lyric = null)) }).track(TRACK_ID)

        assertThat(result.lyric).isNull()
    }

    @Test
    fun trackRejectsAResourceForAnotherTrack() = runTest {
        val failure = runCatching {
            dataSource(FakeCatalogApi { Response.success(detail(lyric = lyric(trackId = "other-track"))) })
                .track(TRACK_ID)
        }.exceptionOrNull()

        assertThat(failure).isInstanceOf(CatalogProtocolException::class.java)
    }

    @Test
    fun trackRejectsAnUnknownTimingMarker() = runTest {
        val failure = runCatching {
            dataSource(FakeCatalogApi { Response.success(detail(lyric = lyric(timing = "UNKNOWN"))) })
                .track(TRACK_ID)
        }.exceptionOrNull()

        assertThat(failure).isInstanceOf(CatalogProtocolException::class.java)
    }

    private fun dataSource(api: CatalogApi): HttpCatalogRemoteDataSource = HttpCatalogRemoteDataSource(
        api = api,
        problemResponseParser = ProblemResponseParser(Json { ignoreUnknownKeys = true }, ProblemMapper()),
    )

    private fun detail(lyric: LyricsResourceDto?) = TrackDetailDto(
        id = TRACK_ID,
        title = "Track",
        artists = emptyList(),
        album = null,
        artwork = null,
        durationMs = 180_000,
        trackNumber = 1,
        discNumber = 1,
        isFavorite = false,
        publishedAt = TIMESTAMP,
        lyric = lyric,
    )

    private fun lyric(
        trackId: String = TRACK_ID,
        timing: String = "LINE",
    ) = LyricsResourceDto(
        id = "lyric-1",
        trackId = trackId,
        language = "zh-CN",
        format = "PLAIN",
        timing = timing,
        content = "Line",
        trackVersion = 1,
        updatedAt = TIMESTAMP,
    )

    private class FakeCatalogApi(
        private val trackHandler: suspend (String) -> Response<TrackDetailDto>,
    ) : CatalogApi {
        override suspend fun tracks(
            cursor: String?,
            limit: Int,
            artistId: String?,
            albumId: String?,
            sort: String,
        ): Response<TrackPageDto> = error("unused")

        override suspend fun randomTracks(request: RandomCatalogRequestDto): Response<RandomTracksResponseDto> =
            error("unused")

        override suspend fun track(trackId: String): Response<TrackDetailDto> = trackHandler(trackId)

        override suspend fun artists(cursor: String?, limit: Int, sort: String): Response<ArtistPageDto> =
            error("unused")

        override suspend fun artist(artistId: String): Response<ArtistDetailDto> = error("unused")

        override suspend fun albums(
            cursor: String?,
            limit: Int,
            artistId: String?,
            sort: String,
        ): Response<AlbumPageDto> = error("unused")

        override suspend fun randomAlbums(request: RandomCatalogRequestDto): Response<RandomAlbumsResponseDto> =
            error("unused")

        override suspend fun album(albumId: String): Response<AlbumDetailDto> = error("unused")
    }

    private companion object {
        const val TRACK_ID = "track-1"
        const val TIMESTAMP = "2026-01-01T00:00:00Z"
    }
}
