package com.xymusic.app.feature.search.data.remote

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.data.media.remote.AlbumPageDto
import com.xymusic.app.core.data.media.remote.ArtistPageDto
import com.xymusic.app.core.data.media.remote.TrackPageDto
import com.xymusic.app.core.network.ProblemMapper
import com.xymusic.app.data.network.ProblemResponseParser
import com.xymusic.app.feature.search.domain.model.SearchQuery
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import org.junit.Test
import retrofit2.Response

class SearchRemoteDataSourceTest {
    @Test
    fun overviewUsesAllScopeWithoutCursorAndRequiresThreeSections() = runTest {
        val api =
            FakeSearchApi { query, scope, cursor, limit ->
                assertThat(query).isEqualTo("Music")
                assertThat(scope).isEqualTo("ALL")
                assertThat(cursor).isNull()
                assertThat(limit).isEqualTo(5)
                Response.success(allResponse(query))
            }

        val overview = dataSource(api).overview(SearchQuery.from("Music"))

        assertThat(overview.tracks).isEmpty()
        assertThat(overview.artists).isEmpty()
        assertThat(overview.albums).isEmpty()
    }

    @Test
    fun specificScopeUsesTwentyItemsAndForwardsCursor() = runTest {
        val api =
            FakeSearchApi { query, scope, cursor, limit ->
                assertThat(scope).isEqualTo("TRACKS")
                assertThat(cursor).isEqualTo("cursor-1")
                assertThat(limit).isEqualTo(20)
                Response.success(
                    CatalogSearchResponseDto(
                        query = query,
                        scope = scope,
                        tracks = TrackPageDto(emptyList(), null),
                    ),
                )
            }

        val page = dataSource(api).tracks("cursor-1", SearchQuery.from("Music"))

        assertThat(page.items).isEmpty()
        assertThat(page.nextCursor).isNull()
    }

    @Test
    fun mismatchedResponseQueryIsAProtocolFailure() = runTest {
        val api = FakeSearchApi { _, _, _, _ -> Response.success(allResponse("Other")) }

        val failure =
            runCatching {
                dataSource(api).overview(SearchQuery.from("Music"))
            }.exceptionOrNull()

        assertThat(failure).isInstanceOf(SearchProtocolException::class.java)
    }

    @Test
    fun missingSelectedSectionIsAProtocolFailure() = runTest {
        val api =
            FakeSearchApi { query, scope, _, _ ->
                Response.success(CatalogSearchResponseDto(query = query, scope = scope))
            }

        val failure =
            runCatching {
                dataSource(api).artists(null, SearchQuery.from("Music"))
            }.exceptionOrNull()

        assertThat(failure).isInstanceOf(SearchProtocolException::class.java)
    }

    private fun dataSource(api: SearchApi): HttpSearchRemoteDataSource {
        val json = Json { ignoreUnknownKeys = true }
        return HttpSearchRemoteDataSource(api, ProblemResponseParser(json, ProblemMapper()))
    }

    private fun allResponse(query: String) = CatalogSearchResponseDto(
        query = query,
        scope = "ALL",
        tracks = TrackPageDto(emptyList(), null),
        artists = ArtistPageDto(emptyList(), null),
        albums = AlbumPageDto(emptyList(), null),
    )

    private fun interface SearchHandler {
        suspend fun invoke(
            query: String,
            scope: String,
            cursor: String?,
            limit: Int,
        ): Response<CatalogSearchResponseDto>
    }

    private class FakeSearchApi(private val handler: SearchHandler) : SearchApi {
        override suspend fun search(
            query: String,
            scope: String,
            cursor: String?,
            limit: Int,
        ): Response<CatalogSearchResponseDto> = handler.invoke(query, scope, cursor, limit)
    }
}
