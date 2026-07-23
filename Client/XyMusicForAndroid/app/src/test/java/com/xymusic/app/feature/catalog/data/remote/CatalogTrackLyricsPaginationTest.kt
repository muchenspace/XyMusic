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

class CatalogTrackLyricsPaginationTest {
    @Test
    fun trackLoadsEveryLyricPageBeforeReturning() = runTest {
        val requestedPages = mutableListOf<Int>()
        val api =
            FakeCatalogApi { _, page, pageSize ->
                requestedPages += page
                assertThat(pageSize).isEqualTo(100)
                Response.success(
                    detail(
                        page = page,
                        lyrics =
                        if (page == 1) {
                            (0 until 100).map(::lyric)
                        } else {
                            listOf(lyric(100))
                        },
                    ),
                )
            }

        val result = dataSource(api).track(TRACK_ID)

        assertThat(requestedPages).containsExactly(1, 2).inOrder()
        assertThat(result.lyrics).hasSize(101)
    }

    @Test
    fun duplicateLyricAcrossPagesIsRejected() = runTest {
        val api =
            FakeCatalogApi { _, page, _ ->
                Response.success(
                    detail(
                        page = page,
                        lyrics =
                        if (page == 1) {
                            (0 until 100).map(::lyric)
                        } else {
                            listOf(lyric(0))
                        },
                    ),
                )
            }

        val failure = runCatching { dataSource(api).track(TRACK_ID) }.exceptionOrNull()

        assertThat(failure).isInstanceOf(CatalogProtocolException::class.java)
    }

    @Test
    fun changedTrackVersionAcrossLyricPagesIsRejected() = runTest {
        val api =
            FakeCatalogApi { _, page, _ ->
                Response.success(
                    detail(
                        page = page,
                        lyrics =
                        if (page == 1) {
                            (0 until 100).map(::lyric)
                        } else {
                            listOf(lyric(100, trackVersion = 2))
                        },
                    ),
                )
            }

        val failure = runCatching { dataSource(api).track(TRACK_ID) }.exceptionOrNull()

        assertThat(failure).isInstanceOf(CatalogProtocolException::class.java)
    }

    private fun dataSource(api: CatalogApi): HttpCatalogRemoteDataSource = HttpCatalogRemoteDataSource(
        api = api,
        problemResponseParser = ProblemResponseParser(Json { ignoreUnknownKeys = true }, ProblemMapper()),
    )

    private fun detail(page: Int, lyrics: List<LyricsResourceDto>) = TrackDetailDto(
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
        lyrics = lyrics,
        lyricPage = page,
        lyricPageSize = 100,
        lyricTotal = 101,
        lyricTotalPages = 2,
    )

    private fun lyric(index: Int, trackVersion: Long = 1) = LyricsResourceDto(
        id = "lyric-$index",
        trackId = TRACK_ID,
        language = "zh-CN",
        format = "PLAIN",
        content = "Line $index",
        isDefault = index == 0,
        trackVersion = trackVersion,
        updatedAt = TIMESTAMP,
    )

    private fun interface TrackHandler {
        suspend fun invoke(trackId: String, page: Int, pageSize: Int): Response<TrackDetailDto>
    }

    private class FakeCatalogApi(private val trackHandler: TrackHandler) : CatalogApi {
        override suspend fun tracks(
            cursor: String?,
            limit: Int,
            artistId: String?,
            albumId: String?,
            sort: String,
        ): Response<TrackPageDto> = error("unused")

        override suspend fun randomTracks(request: RandomCatalogRequestDto): Response<RandomTracksResponseDto> =
            error("unused")

        override suspend fun track(trackId: String, lyricPage: Int, lyricPageSize: Int): Response<TrackDetailDto> =
            trackHandler.invoke(trackId, lyricPage, lyricPageSize)

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
