package com.xymusic.app.feature.catalog.data.remote

import com.xymusic.app.core.data.media.remote.AlbumDetailDto
import com.xymusic.app.core.data.media.remote.AlbumSummaryDto
import com.xymusic.app.core.data.media.remote.ArtistDetailDto
import com.xymusic.app.core.data.media.remote.ArtistSummaryDto
import com.xymusic.app.core.data.media.remote.CatalogProtocolException
import com.xymusic.app.core.data.media.remote.RandomCatalogRequestDto
import com.xymusic.app.core.data.media.remote.RemotePage
import com.xymusic.app.core.data.media.remote.TrackDetailDto
import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import com.xymusic.app.data.network.ProblemResponseParser
import com.xymusic.app.domain.error.DomainError
import com.xymusic.app.feature.catalog.domain.model.AlbumQuery
import com.xymusic.app.feature.catalog.domain.model.ArtistQuery
import com.xymusic.app.feature.catalog.domain.model.TrackQuery
import java.io.IOException
import javax.inject.Inject
import retrofit2.Response

interface CatalogRemoteDataSource {
    suspend fun tracks(cursor: String?, limit: Int, query: TrackQuery): RemotePage<TrackSummaryDto>

    suspend fun randomTracks(limit: Int): List<TrackSummaryDto>

    suspend fun artists(cursor: String?, limit: Int, query: ArtistQuery): RemotePage<ArtistSummaryDto>

    suspend fun albums(cursor: String?, limit: Int, query: AlbumQuery): RemotePage<AlbumSummaryDto>

    suspend fun randomAlbums(limit: Int): List<AlbumSummaryDto>

    suspend fun track(trackId: String): TrackDetailDto

    suspend fun artist(artistId: String): ArtistDetailDto

    suspend fun album(albumId: String): AlbumDetailDto
}

class HttpCatalogRemoteDataSource
@Inject
constructor(
    private val api: CatalogApi,
    private val problemResponseParser: ProblemResponseParser,
) : CatalogRemoteDataSource {
    override suspend fun tracks(cursor: String?, limit: Int, query: TrackQuery): RemotePage<TrackSummaryDto> {
        val page =
            body(
                api.tracks(
                    cursor = cursor,
                    limit = limit,
                    artistId = query.artistId,
                    albumId = query.albumId,
                    sort = query.sort.name,
                ),
            )
        return RemotePage(page.items, page.nextCursor)
    }

    override suspend fun randomTracks(limit: Int): List<TrackSummaryDto> {
        requireRandomLimit(limit)
        return body(api.randomTracks(RandomCatalogRequestDto(limit))).items
    }

    override suspend fun artists(cursor: String?, limit: Int, query: ArtistQuery): RemotePage<ArtistSummaryDto> {
        val page = body(api.artists(cursor, limit, query.sort.name))
        return RemotePage(page.items, page.nextCursor)
    }

    override suspend fun albums(cursor: String?, limit: Int, query: AlbumQuery): RemotePage<AlbumSummaryDto> {
        val page = body(api.albums(cursor, limit, query.artistId, query.sort.name))
        return RemotePage(page.items, page.nextCursor)
    }

    override suspend fun randomAlbums(limit: Int): List<AlbumSummaryDto> {
        requireRandomLimit(limit)
        return body(api.randomAlbums(RandomCatalogRequestDto(limit))).items
    }

    override suspend fun track(trackId: String): TrackDetailDto {
        val detail = body(api.track(trackId))
        validateLyric(detail, trackId)
        return detail
    }

    override suspend fun artist(artistId: String): ArtistDetailDto = body(api.artist(artistId))

    override suspend fun album(albumId: String): AlbumDetailDto = body(api.album(albumId))

    private fun <T> body(response: Response<T>): T {
        if (!response.isSuccessful) {
            throw CatalogRemoteException(
                problemResponseParser.parse(
                    status = response.code(),
                    body = response.errorBody()?.string(),
                    traceId = response.headers()[TRACE_ID_HEADER],
                    retryAfterSeconds = response.headers()[RETRY_AFTER_HEADER]?.toLongOrNull(),
                ),
            )
        }
        return response.body() ?: throw CatalogProtocolException("Catalog response body is missing")
    }

    private companion object {
        const val TRACE_ID_HEADER = "X-Trace-Id"
        const val RETRY_AFTER_HEADER = "Retry-After"
        const val MIN_RANDOM_LIMIT = 1
        const val MAX_RANDOM_LIMIT = 50

        fun requireRandomLimit(limit: Int) {
            require(limit in MIN_RANDOM_LIMIT..MAX_RANDOM_LIMIT) {
                "Random catalog limit must be between $MIN_RANDOM_LIMIT and $MAX_RANDOM_LIMIT"
            }
        }

        fun validateLyric(detail: TrackDetailDto, trackId: String) {
            requireCatalogProtocol(
                detail.id == trackId,
                "Track detail ID mismatch",
            )
            val lyric = detail.lyric ?: return
            requireCatalogProtocol(lyric.id.isNotBlank(), "Track lyric ID is missing")
            requireCatalogProtocol(lyric.trackId == trackId, "Track lyric belongs to another track")
            requireCatalogProtocol(lyric.trackVersion >= 1, "Track lyric version must be positive")
            requireCatalogProtocol(
                lyric.timing == "LINE" || lyric.timing == "WORD",
                "Track lyric timing is invalid",
            )
        }

        fun requireCatalogProtocol(condition: Boolean, message: String) {
            if (!condition) {
                throw CatalogProtocolException(message)
            }
        }
    }
}

class CatalogRemoteException(val domainError: DomainError) : IOException("Catalog request was rejected")
