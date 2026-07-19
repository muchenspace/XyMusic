package com.xymusic.app.feature.playlist.data.remote

import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.network.model.ProblemCode
import com.xymusic.app.data.network.ProblemResponseParser
import com.xymusic.app.feature.playlist.domain.model.PlaylistVersionConflict
import java.io.IOException
import javax.inject.Inject
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.longOrNull
import retrofit2.Response

interface PlaylistRemoteDataSource {
    suspend fun allPlaylists(sort: String): List<PlaylistSummaryDto>

    suspend fun playlist(playlistId: String): PlaylistDetailDto

    suspend fun playlistPage(playlistId: String, cursor: String?, limit: Int): PlaylistDetailDto {
        require(cursor == null) { "This playlist data source does not support continuation pages" }
        return playlist(playlistId)
    }

    suspend fun playlistProgressively(
        playlistId: String,
        onFirstPage: suspend (PlaylistDetailDto) -> Unit,
    ): PlaylistDetailDto = playlist(playlistId).also { onFirstPage(it) }

    suspend fun create(idempotencyKey: String, request: CreatePlaylistRequestDto): PlaylistSummaryDto

    suspend fun update(playlistId: String, idempotencyKey: String, payload: PlaylistUpdatePayload): PlaylistSummaryDto

    suspend fun delete(playlistId: String, expectedVersion: Long, idempotencyKey: String)

    suspend fun addTrack(
        playlistId: String,
        idempotencyKey: String,
        request: AddPlaylistTrackRequestDto,
    ): PlaylistEntryMutationDto

    suspend fun removeTrack(
        playlistId: String,
        entryId: String,
        expectedVersion: Long,
        idempotencyKey: String,
    ): PlaylistMutationDto

    suspend fun reorder(
        playlistId: String,
        idempotencyKey: String,
        request: ReorderPlaylistRequestDto,
    ): PlaylistMutationDto
}

class HttpPlaylistRemoteDataSource
@Inject
constructor(
    private val api: PlaylistApi,
    private val problemResponseParser: ProblemResponseParser,
    private val json: Json,
) : PlaylistRemoteDataSource {
    override suspend fun allPlaylists(sort: String): List<PlaylistSummaryDto> = collectPages(
        load = { cursor -> body(api.playlists(cursor, PAGE_LIMIT, sort), null, null) },
        itemSelector = PlaylistPageDto::items,
        nextCursor = PlaylistPageDto::nextCursor,
    )

    override suspend fun playlist(playlistId: String): PlaylistDetailDto = loadPlaylist(playlistId, null)

    override suspend fun playlistPage(playlistId: String, cursor: String?, limit: Int): PlaylistDetailDto {
        require(limit in 1..PAGE_LIMIT)
        return body(api.playlist(playlistId, cursor, limit), playlistId, null)
    }

    override suspend fun playlistProgressively(
        playlistId: String,
        onFirstPage: suspend (PlaylistDetailDto) -> Unit,
    ): PlaylistDetailDto = loadPlaylist(playlistId, onFirstPage)

    private suspend fun loadPlaylist(
        playlistId: String,
        onFirstPage: (suspend (PlaylistDetailDto) -> Unit)?,
    ): PlaylistDetailDto {
        var first: PlaylistDetailDto? = null
        val entries = mutableListOf<PlaylistEntryDto>()
        val entryIds = mutableSetOf<String>()
        val seenCursors = mutableSetOf<String>()
        var cursor: String? = null
        var pageCount = 0
        do {
            val page = playlistPage(playlistId, cursor, PAGE_LIMIT)
            val metadata = first
            if (metadata == null) {
                first = page
            } else {
                require(page.sameResourceAs(metadata)) { "Playlist metadata changed while paging" }
            }
            require(page.entries.all { entryIds.add(it.id) }) {
                "Playlist paging returned duplicate entry IDs"
            }
            entries += page.entries
            pageCount += 1
            val next = page.nextCursor
            if (next != null && !seenCursors.add(next)) {
                throw PlaylistProtocolException("Playlist pagination cursor repeated")
            }
            if (pageCount == 1 && next != null) {
                onFirstPage?.invoke(page.copy(entries = entries.toList()))
            }
            cursor = next
        } while (cursor != null)

        return requireNotNull(first).copy(
            entries = entries.sortedBy(PlaylistEntryDto::position),
            nextCursor = null,
        )
    }

    override suspend fun create(idempotencyKey: String, request: CreatePlaylistRequestDto): PlaylistSummaryDto =
        body(api.create(idempotencyKey, request), null, null)

    override suspend fun update(
        playlistId: String,
        idempotencyKey: String,
        payload: PlaylistUpdatePayload,
    ): PlaylistSummaryDto {
        val request =
            buildJsonObject {
                put("expectedVersion", JsonPrimitive(payload.expectedVersion))
                if (payload.namePresent) put("name", JsonPrimitive(requireNotNull(payload.name)))
                if (payload.descriptionPresent) {
                    put("description", payload.description?.let(::JsonPrimitive) ?: JsonNull)
                }
                if (payload.visibilityPresent) {
                    put("visibility", JsonPrimitive(requireNotNull(payload.visibility)))
                }
            }
        return body(api.update(playlistId, idempotencyKey, request), playlistId, payload.expectedVersion)
    }

    override suspend fun delete(playlistId: String, expectedVersion: Long, idempotencyKey: String) {
        successful(api.delete(playlistId, expectedVersion, idempotencyKey), playlistId, expectedVersion)
    }

    override suspend fun addTrack(
        playlistId: String,
        idempotencyKey: String,
        request: AddPlaylistTrackRequestDto,
    ): PlaylistEntryMutationDto = body(
        api.addTrack(playlistId, idempotencyKey, request),
        playlistId,
        request.expectedVersion,
    )

    override suspend fun removeTrack(
        playlistId: String,
        entryId: String,
        expectedVersion: Long,
        idempotencyKey: String,
    ): PlaylistMutationDto = body(
        api.removeTrack(playlistId, entryId, expectedVersion, idempotencyKey),
        playlistId,
        expectedVersion,
    )

    override suspend fun reorder(
        playlistId: String,
        idempotencyKey: String,
        request: ReorderPlaylistRequestDto,
    ): PlaylistMutationDto = body(
        api.reorder(playlistId, idempotencyKey, request),
        playlistId,
        request.expectedVersion,
    )

    private suspend fun <P, T> collectPages(
        load: suspend (String?) -> P,
        itemSelector: (P) -> List<T>,
        nextCursor: (P) -> String?,
    ): List<T> {
        val collected = mutableListOf<T>()
        val seenCursors = mutableSetOf<String>()
        var cursor: String? = null
        do {
            val page = load(cursor)
            collected += itemSelector(page)
            val next = nextCursor(page)
            if (next != null && !seenCursors.add(next)) {
                throw PlaylistProtocolException("Playlist pagination cursor repeated")
            }
            cursor = next
        } while (cursor != null)
        return collected
    }

    private fun <T> body(response: Response<T>, playlistId: String?, expectedVersion: Long?): T {
        successful(response, playlistId, expectedVersion)
        return response.body() ?: throw PlaylistProtocolException("Playlist response body is missing")
    }

    private fun successful(response: Response<*>, playlistId: String?, expectedVersion: Long?) {
        if (response.isSuccessful) return
        val body = response.errorBody()?.string()
        val error =
            problemResponseParser.parse(
                status = response.code(),
                body = body,
                traceId = response.headers()[TRACE_ID_HEADER],
                retryAfterSeconds = response.headers()[RETRY_AFTER_HEADER]?.toLongOrNull(),
            )
        val conflict =
            if (
                error is DomainError.Conflict &&
                error.reason == ProblemCode.VersionConflict &&
                playlistId != null &&
                expectedVersion != null
            ) {
                parseConflict(body, playlistId, expectedVersion)
            } else {
                null
            }
        throw PlaylistRemoteException(error, conflict)
    }

    private fun parseConflict(body: String?, playlistId: String, expectedVersion: Long): PlaylistVersionConflict {
        val objectValue =
            runCatching {
                body?.let(json::parseToJsonElement)?.jsonObject
            }.getOrNull()
        return PlaylistVersionConflict(
            playlistId =
            objectValue?.get("conflictResourceId")?.jsonPrimitive?.contentOrNull
                ?: playlistId,
            expectedVersion =
            objectValue?.get("expectedVersion")?.jsonPrimitive?.longOrNull
                ?: expectedVersion,
            currentVersion = objectValue?.get("currentVersion")?.jsonPrimitive?.longOrNull,
            conflictFields =
            objectValue
                ?.get("conflictFields")
                ?.jsonArray
                ?.mapNotNull { it.jsonPrimitive.contentOrNull }
                ?.toSet()
                .orEmpty(),
        )
    }

    private fun PlaylistDetailDto.sameResourceAs(other: PlaylistDetailDto): Boolean =
        copy(entries = emptyList(), nextCursor = null) == other.copy(entries = emptyList(), nextCursor = null)

    private companion object {
        const val PAGE_LIMIT = 100
        const val TRACE_ID_HEADER = "X-Trace-Id"
        const val RETRY_AFTER_HEADER = "Retry-After"
    }
}

class PlaylistRemoteException(val error: DomainError, val conflict: PlaylistVersionConflict?) :
    IOException("Playlist request was rejected")

class PlaylistProtocolException(message: String, cause: Throwable? = null) : IOException(message, cause)
