package com.xymusic.app.feature.playlist.data

import com.xymusic.app.feature.playlist.data.remote.PlaylistDetailDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistEntryDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistProtocolException

internal data class PlaylistPageMergeResult(
    val page: PlaylistDetailDto,
    val completeDetail: PlaylistDetailDto?,
)

internal class PlaylistPageAccumulator private constructor(
    private val firstPage: PlaylistDetailDto,
    private val entries: MutableList<PlaylistEntryDto>,
    private val entryIds: MutableSet<String>,
    private val positions: MutableSet<Int>,
    private val seenCursors: MutableSet<String>,
    var nextCursor: String?,
) {
    val resourceVersion: Long
        get() = firstPage.version

    fun append(requestedCursor: String, page: PlaylistDetailDto): PlaylistPageMergeResult {
        if (requestedCursor != nextCursor) {
            throw PlaylistProtocolException("Playlist continuation cursor does not match the active page")
        }
        validateMetadata(page)
        validatePage(requestedCursor, page)
        page.entries.forEach { entry ->
            if (!entryIds.add(entry.id)) {
                throw PlaylistProtocolException("Playlist paging returned duplicate entry IDs")
            }
            if (!positions.add(entry.position)) {
                throw PlaylistProtocolException("Playlist paging returned duplicate entry positions")
            }
        }
        entries += page.entries
        if (entries.size > firstPage.trackCount) {
            throw PlaylistProtocolException("Playlist paging returned more entries than trackCount")
        }
        nextCursor = page.nextCursor
        validateCompletion()
        return PlaylistPageMergeResult(page, completeDetailOrNull())
    }

    private fun validateMetadata(page: PlaylistDetailDto) {
        if (!page.sameResourceAs(firstPage)) {
            throw PlaylistProtocolException("Playlist metadata changed while paging")
        }
    }

    private fun validateCompletion() {
        when {
            nextCursor == null && entries.size != firstPage.trackCount ->
                throw PlaylistProtocolException("Playlist paging ended before every entry was returned")
            nextCursor != null && entries.size >= firstPage.trackCount ->
                throw PlaylistProtocolException("Playlist paging continued after every entry was returned")
        }
    }

    private fun completeDetailOrNull(): PlaylistDetailDto? =
        if (nextCursor == null) {
            firstPage.copy(
                entries = entries.sortedBy(PlaylistEntryDto::position),
                nextCursor = null,
            )
        } else {
            null
        }

    companion object {
        fun start(playlistId: String, page: PlaylistDetailDto): Pair<PlaylistPageAccumulator?, PlaylistPageMergeResult> {
            if (page.id != playlistId) {
                throw PlaylistProtocolException("Playlist detail ID does not match the request")
            }
            validatePage(requestedCursor = null, page = page)
            val entryIds = page.entries.mapTo(linkedSetOf(), PlaylistEntryDto::id)
            if (entryIds.size != page.entries.size) {
                throw PlaylistProtocolException("Playlist paging returned duplicate entry IDs")
            }
            val positions = page.entries.mapTo(linkedSetOf(), PlaylistEntryDto::position)
            if (positions.size != page.entries.size) {
                throw PlaylistProtocolException("Playlist paging returned duplicate entry positions")
            }
            if (page.entries.size > page.trackCount) {
                throw PlaylistProtocolException("Playlist paging returned more entries than trackCount")
            }
            val accumulator =
                PlaylistPageAccumulator(
                    firstPage = page,
                    entries = page.entries.toMutableList(),
                    entryIds = entryIds,
                    positions = positions,
                    seenCursors = page.nextCursor?.let { cursor -> mutableSetOf(cursor) } ?: mutableSetOf(),
                    nextCursor = page.nextCursor,
                )
            accumulator.validateCompletion()
            val complete = accumulator.completeDetailOrNull()
            return (accumulator.takeIf { complete == null }) to PlaylistPageMergeResult(page, complete)
        }

        private fun validatePage(requestedCursor: String?, page: PlaylistDetailDto) {
            if (page.entries.isEmpty() && page.nextCursor != null) {
                throw PlaylistProtocolException("Empty playlist page cannot have a next cursor")
            }
            if (page.nextCursor != null && page.nextCursor == requestedCursor) {
                throw PlaylistProtocolException("Playlist pagination cursor did not advance")
            }
        }
    }

    private fun validatePage(requestedCursor: String, page: PlaylistDetailDto) {
        Companion.validatePage(requestedCursor, page)
        page.nextCursor?.let { next ->
            if (!seenCursors.add(next)) {
                throw PlaylistProtocolException("Playlist pagination cursor repeated")
            }
        }
    }
}

private fun PlaylistDetailDto.sameResourceAs(other: PlaylistDetailDto): Boolean =
    copy(entries = emptyList(), nextCursor = null) == other.copy(entries = emptyList(), nextCursor = null)
