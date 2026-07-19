package com.xymusic.app.feature.search.data.remote

import com.xymusic.app.core.data.media.remote.AlbumSummaryDto
import com.xymusic.app.core.data.media.remote.ArtistSummaryDto
import com.xymusic.app.core.data.media.remote.RemotePage
import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.data.network.ProblemResponseParser
import com.xymusic.app.feature.search.domain.model.SearchQuery
import com.xymusic.app.feature.search.domain.model.SearchScope
import java.io.IOException
import javax.inject.Inject
import retrofit2.Response

interface SearchRemoteDataSource {
    suspend fun overview(query: SearchQuery): SearchOverviewRemote

    suspend fun tracks(cursor: String?, query: SearchQuery): RemotePage<TrackSummaryDto>

    suspend fun artists(cursor: String?, query: SearchQuery): RemotePage<ArtistSummaryDto>

    suspend fun albums(cursor: String?, query: SearchQuery): RemotePage<AlbumSummaryDto>
}

class HttpSearchRemoteDataSource
@Inject
constructor(
    private val api: SearchApi,
    private val problemResponseParser: ProblemResponseParser,
) : SearchRemoteDataSource {
    override suspend fun overview(query: SearchQuery): SearchOverviewRemote {
        val response =
            body(
                api.search(query.value, SearchScope.ALL.name, cursor = null, limit = OVERVIEW_LIMIT),
            )
        validateEnvelope(response, query, SearchScope.ALL)
        val tracks = response.tracks ?: throw SearchProtocolException("ALL search is missing tracks")
        val artists = response.artists ?: throw SearchProtocolException("ALL search is missing artists")
        val albums = response.albums ?: throw SearchProtocolException("ALL search is missing albums")
        protocolCheck(tracks.items.size <= OVERVIEW_LIMIT, "ALL track section exceeds its limit")
        protocolCheck(artists.items.size <= OVERVIEW_LIMIT, "ALL artist section exceeds its limit")
        protocolCheck(albums.items.size <= OVERVIEW_LIMIT, "ALL album section exceeds its limit")
        requireUniqueIds(tracks.items.map(TrackSummaryDto::id), "track")
        requireUniqueIds(artists.items.map(ArtistSummaryDto::id), "artist")
        requireUniqueIds(albums.items.map(AlbumSummaryDto::id), "album")
        return SearchOverviewRemote(tracks.items, artists.items, albums.items)
    }

    override suspend fun tracks(cursor: String?, query: SearchQuery): RemotePage<TrackSummaryDto> {
        val response =
            body(
                api.search(query.value, SearchScope.TRACKS.name, cursor, SPECIFIC_SCOPE_LIMIT),
            )
        validateEnvelope(response, query, SearchScope.TRACKS)
        protocolCheck(
            response.artists == null && response.albums == null,
            "TRACKS search returned unrelated sections",
        )
        val page = response.tracks ?: throw SearchProtocolException("TRACKS search is missing tracks")
        validateSpecificPage(page.items.map(TrackSummaryDto::id), page.items.size, "track")
        return RemotePage(page.items, page.nextCursor)
    }

    override suspend fun artists(cursor: String?, query: SearchQuery): RemotePage<ArtistSummaryDto> {
        val response =
            body(
                api.search(query.value, SearchScope.ARTISTS.name, cursor, SPECIFIC_SCOPE_LIMIT),
            )
        validateEnvelope(response, query, SearchScope.ARTISTS)
        protocolCheck(
            response.tracks == null && response.albums == null,
            "ARTISTS search returned unrelated sections",
        )
        val page = response.artists ?: throw SearchProtocolException("ARTISTS search is missing artists")
        validateSpecificPage(page.items.map(ArtistSummaryDto::id), page.items.size, "artist")
        return RemotePage(page.items, page.nextCursor)
    }

    override suspend fun albums(cursor: String?, query: SearchQuery): RemotePage<AlbumSummaryDto> {
        val response =
            body(
                api.search(query.value, SearchScope.ALBUMS.name, cursor, SPECIFIC_SCOPE_LIMIT),
            )
        validateEnvelope(response, query, SearchScope.ALBUMS)
        protocolCheck(
            response.tracks == null && response.artists == null,
            "ALBUMS search returned unrelated sections",
        )
        val page = response.albums ?: throw SearchProtocolException("ALBUMS search is missing albums")
        validateSpecificPage(page.items.map(AlbumSummaryDto::id), page.items.size, "album")
        return RemotePage(page.items, page.nextCursor)
    }

    private fun validateEnvelope(response: CatalogSearchResponseDto, query: SearchQuery, scope: SearchScope) {
        val responseQuery =
            runCatching { SearchQuery.from(response.query) }
                .getOrElse { throw SearchProtocolException("Search response query is invalid", it) }
        if (responseQuery.normalizedValue != query.normalizedValue) {
            throw SearchProtocolException("Search response query does not match the request")
        }
        if (response.scope != scope.name) {
            throw SearchProtocolException("Search response scope does not match the request")
        }
    }

    private fun validateSpecificPage(ids: List<String>, size: Int, label: String) {
        protocolCheck(size <= SPECIFIC_SCOPE_LIMIT, "$label search page exceeds its limit")
        requireUniqueIds(ids, label)
    }

    private fun requireUniqueIds(ids: List<String>, label: String) {
        if (ids.distinct().size != ids.size) {
            throw SearchProtocolException("Search response contains duplicate $label IDs")
        }
    }

    private fun protocolCheck(condition: Boolean, message: String) {
        if (!condition) throw SearchProtocolException(message)
    }

    private fun <T> body(response: Response<T>): T {
        if (!response.isSuccessful) {
            throw SearchRemoteException(
                problemResponseParser.parse(
                    status = response.code(),
                    body = response.errorBody()?.string(),
                    traceId = response.headers()[TRACE_ID_HEADER],
                    retryAfterSeconds = response.headers()[RETRY_AFTER_HEADER]?.toLongOrNull(),
                ),
            )
        }
        return response.body() ?: throw SearchProtocolException("Search response body is missing")
    }

    private companion object {
        const val OVERVIEW_LIMIT = 5
        const val SPECIFIC_SCOPE_LIMIT = 20
        const val TRACE_ID_HEADER = "X-Trace-Id"
        const val RETRY_AFTER_HEADER = "Retry-After"
    }
}

data class SearchOverviewRemote(
    val tracks: List<TrackSummaryDto>,
    val artists: List<ArtistSummaryDto>,
    val albums: List<AlbumSummaryDto>,
)

class SearchRemoteException(val domainError: DomainError) : IOException("Search request was rejected")

class SearchProtocolException(message: String, cause: Throwable? = null) : IOException(message, cause)
