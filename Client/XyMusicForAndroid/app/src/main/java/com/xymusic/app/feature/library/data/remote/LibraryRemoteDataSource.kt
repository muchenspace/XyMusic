package com.xymusic.app.feature.library.data.remote

import com.xymusic.app.core.network.DomainError
import com.xymusic.app.data.network.ProblemResponseParser
import java.io.IOException
import javax.inject.Inject
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.flow
import retrofit2.Response

interface LibraryRemoteDataSource {
    suspend fun allFavorites(sort: String): List<FavoriteItemDto>

    suspend fun addFavorite(trackId: String): FavoriteItemDto

    suspend fun removeFavorite(trackId: String)

    fun historyPages(): Flow<List<HistoryItemDto>>

    suspend fun recordPlayback(
        trackId: String,
        idempotencyKey: String,
        request: RecordPlaybackRequestDto,
    ): HistoryItemDto
}

class HttpLibraryRemoteDataSource
@Inject
constructor(
    private val api: LibraryApi,
    private val problemResponseParser: ProblemResponseParser,
) : LibraryRemoteDataSource {
    override suspend fun allFavorites(sort: String): List<FavoriteItemDto> = collectPages(
        load = { cursor -> body(api.favorites(cursor, PAGE_LIMIT, sort)) },
        itemSelector = FavoritePageDto::items,
        nextCursor = FavoritePageDto::nextCursor,
    )

    override suspend fun addFavorite(trackId: String): FavoriteItemDto = body(api.addFavorite(trackId))

    override suspend fun removeFavorite(trackId: String) {
        successful(api.removeFavorite(trackId))
    }

    override fun historyPages(): Flow<List<HistoryItemDto>> = pages(
        load = { cursor -> body(api.history(cursor, PAGE_LIMIT)) },
        itemSelector = HistoryPageDto::items,
        nextCursor = HistoryPageDto::nextCursor,
    )

    override suspend fun recordPlayback(
        trackId: String,
        idempotencyKey: String,
        request: RecordPlaybackRequestDto,
    ): HistoryItemDto = body(api.recordPlayback(trackId, idempotencyKey, request))

    private suspend fun <P, T> collectPages(
        load: suspend (String?) -> P,
        itemSelector: (P) -> List<T>,
        nextCursor: (P) -> String?,
    ): List<T> {
        val collected = mutableListOf<T>()
        pages(load, itemSelector, nextCursor).collect(collected::addAll)
        return collected
    }

    private fun <P, T> pages(
        load: suspend (String?) -> P,
        itemSelector: (P) -> List<T>,
        nextCursor: (P) -> String?,
    ): Flow<List<T>> = flow {
        val seenCursors = mutableSetOf<String>()
        var cursor: String? = null
        do {
            val page = load(cursor)
            val next = nextCursor(page)
            if (next != null && !seenCursors.add(next)) {
                throw LibraryProtocolException("Library pagination cursor repeated")
            }
            emit(itemSelector(page))
            cursor = next
        } while (cursor != null)
    }

    private fun <T> body(response: Response<T>): T {
        successful(response)
        return response.body() ?: throw LibraryProtocolException("Library response body is missing")
    }

    private fun successful(response: Response<*>) {
        if (response.isSuccessful) return
        throw LibraryRemoteException(
            problemResponseParser.parse(
                status = response.code(),
                body = response.errorBody()?.string(),
                traceId = response.headers()[TRACE_ID_HEADER],
                retryAfterSeconds = response.headers()[RETRY_AFTER_HEADER]?.toLongOrNull(),
            ),
        )
    }

    private companion object {
        const val PAGE_LIMIT = 100
        const val TRACE_ID_HEADER = "X-Trace-Id"
        const val RETRY_AFTER_HEADER = "Retry-After"
    }
}

class LibraryRemoteException(val error: DomainError) : IOException("Library request was rejected")

class LibraryProtocolException(message: String, cause: Throwable? = null) : IOException(message, cause)
