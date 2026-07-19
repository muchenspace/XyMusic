package com.xymusic.app.feature.catalog.data.remote

import com.xymusic.app.core.data.media.remote.AlbumDetailDto
import com.xymusic.app.core.data.media.remote.AlbumSummaryDto
import com.xymusic.app.core.data.media.remote.ArtistDetailDto
import com.xymusic.app.core.data.media.remote.ArtistSummaryDto
import com.xymusic.app.core.data.media.remote.CatalogProtocolException
import com.xymusic.app.core.data.media.remote.LyricsResourceDto
import com.xymusic.app.core.data.media.remote.RandomCatalogRequestDto
import com.xymusic.app.core.data.media.remote.RemotePage
import com.xymusic.app.core.data.media.remote.TrackDetailDto
import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.data.network.ProblemResponseParser
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
        val first = body(api.track(trackId, lyricPage = 1, lyricPageSize = LYRIC_PAGE_SIZE))
        validateLyricPage(first, trackId, page = 1)
        if (first.lyricTotalPages <= 1) return first

        val lyrics = first.lyrics.toMutableList()
        val lyricIds = lyrics.mapTo(hashSetOf(), LyricsResourceDto::id)
        val lyricTrackVersion = first.lyrics.first().trackVersion
        for (pageNumber in 2..first.lyricTotalPages) {
            val page = body(api.track(trackId, lyricPage = pageNumber, lyricPageSize = LYRIC_PAGE_SIZE))
            validateLyricPage(page, trackId, pageNumber)
            if (!page.sameTrackAs(first)) {
                throw CatalogProtocolException("Track metadata changed while paging lyrics")
            }
            if (page.lyrics.any { lyric -> lyric.trackVersion != lyricTrackVersion }) {
                throw CatalogProtocolException("Track lyrics changed version while paging")
            }
            page.lyrics.forEach { lyric ->
                if (!lyricIds.add(lyric.id)) {
                    throw CatalogProtocolException("Track lyric paging returned duplicate lyric IDs")
                }
            }
            lyrics += page.lyrics
        }
        if (lyrics.size != first.lyricTotal) {
            throw CatalogProtocolException("Track lyric paging ended before every lyric was returned")
        }
        return first.copy(lyrics = lyrics)
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
        const val LYRIC_PAGE_SIZE = 100
        const val MIN_RANDOM_LIMIT = 1
        const val MAX_RANDOM_LIMIT = 50

        fun requireRandomLimit(limit: Int) {
            require(limit in MIN_RANDOM_LIMIT..MAX_RANDOM_LIMIT) {
                "Random catalog limit must be between $MIN_RANDOM_LIMIT and $MAX_RANDOM_LIMIT"
            }
        }

        fun validateLyricPage(detail: TrackDetailDto, trackId: String, page: Int) {
            if (detail.id != trackId) {
                throw CatalogProtocolException("Track detail ID mismatch while paging lyrics")
            }
            if (detail.lyricTotal < 0 || detail.lyricTotalPages < 0) {
                throw CatalogProtocolException("Track lyric pagination totals cannot be negative")
            }
            if (detail.lyricPage != page || detail.lyricPageSize != LYRIC_PAGE_SIZE) {
                throw CatalogProtocolException("Track lyric pagination did not return the requested page")
            }
            val expectedPages =
                if (detail.lyricTotal == 0) {
                    0
                } else {
                    ((detail.lyricTotal.toLong() + LYRIC_PAGE_SIZE - 1) / LYRIC_PAGE_SIZE).toInt()
                }
            if (
                detail.lyricTotalPages != expectedPages &&
                !(detail.lyricTotal == 0 && detail.lyricTotalPages == 1)
            ) {
                throw CatalogProtocolException("Track lyric pagination totals are inconsistent")
            }
            if (detail.lyrics.size > LYRIC_PAGE_SIZE) {
                throw CatalogProtocolException("Track lyric page exceeds the requested limit")
            }
            val expectedItemCount =
                when {
                    detail.lyricTotal == 0 -> 0
                    page < detail.lyricTotalPages -> LYRIC_PAGE_SIZE
                    else ->
                        (
                            detail.lyricTotal.toLong() -
                                (detail.lyricTotalPages - 1L) * LYRIC_PAGE_SIZE
                            ).toInt()
                }
            if (detail.lyrics.size != expectedItemCount) {
                throw CatalogProtocolException("Track lyric page item count is inconsistent")
            }
            if (detail.lyrics.map(LyricsResourceDto::id).distinct().size != detail.lyrics.size) {
                throw CatalogProtocolException("Track lyric page contains duplicate lyric IDs")
            }
            if (detail.lyrics.any { lyric -> lyric.trackId != trackId }) {
                throw CatalogProtocolException("Track lyric page contains lyrics for another track")
            }
            if (detail.lyrics.map(LyricsResourceDto::trackVersion).distinct().size > 1) {
                throw CatalogProtocolException("Track lyric page contains multiple track versions")
            }
        }
    }
}

private fun TrackDetailDto.sameTrackAs(other: TrackDetailDto): Boolean =
    id == other.id &&
        title == other.title &&
        artists == other.artists &&
        album == other.album &&
        durationMs == other.durationMs &&
        trackNumber == other.trackNumber &&
        discNumber == other.discNumber &&
        isFavorite == other.isFavorite &&
        publishedAt == other.publishedAt &&
        lyricTotal == other.lyricTotal &&
        lyricTotalPages == other.lyricTotalPages

class CatalogRemoteException(val domainError: DomainError) : IOException("Catalog request was rejected")
