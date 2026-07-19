package com.xymusic.app.feature.playlist.data

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.data.media.remote.ArtistReferenceDto
import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistDetailDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistEntryDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistProtocolException
import com.xymusic.app.feature.playlist.data.remote.UserSummaryDto
import org.junit.Assert.assertThrows
import org.junit.Test

class PlaylistPageAccumulatorTest {
    @Test
    fun firstPageIsAvailableWithoutLoadingTheContinuation() {
        val (accumulator, result) =
            PlaylistPageAccumulator.start(
                PLAYLIST_ID,
                page(trackCount = 2, entries = listOf(entry("entry-1", 0)), nextCursor = "cursor-2"),
            )

        assertThat(accumulator).isNotNull()
        assertThat(result.page.entries.map(PlaylistEntryDto::id)).containsExactly("entry-1")
        assertThat(result.completeDetail).isNull()
    }

    @Test
    fun explicitContinuationCompletesThePlaylist() {
        val (accumulator, _) =
            PlaylistPageAccumulator.start(
                PLAYLIST_ID,
                page(trackCount = 2, entries = listOf(entry("entry-1", 0)), nextCursor = "cursor-2"),
            )

        val result =
            requireNotNull(accumulator).append(
                requestedCursor = "cursor-2",
                page = page(trackCount = 2, entries = listOf(entry("entry-2", 1)), nextCursor = null),
            )

        assertThat(result.completeDetail?.entries?.map(PlaylistEntryDto::id))
            .containsExactly("entry-1", "entry-2")
            .inOrder()
    }

    @Test
    fun duplicateEntryAcrossPagesIsRejected() {
        val (accumulator, _) =
            PlaylistPageAccumulator.start(
                PLAYLIST_ID,
                page(trackCount = 2, entries = listOf(entry("entry-1", 0)), nextCursor = "cursor-2"),
            )

        assertThrows(PlaylistProtocolException::class.java) {
            requireNotNull(accumulator).append(
                requestedCursor = "cursor-2",
                page = page(trackCount = 2, entries = listOf(entry("entry-1", 1)), nextCursor = null),
            )
        }
    }

    @Test
    fun previouslySeenCursorIsRejected() {
        val (accumulator, _) =
            PlaylistPageAccumulator.start(
                PLAYLIST_ID,
                page(trackCount = 3, entries = listOf(entry("entry-1", 0)), nextCursor = "cursor-2"),
            )
        requireNotNull(accumulator).append(
            requestedCursor = "cursor-2",
            page = page(trackCount = 3, entries = listOf(entry("entry-2", 1)), nextCursor = "cursor-3"),
        )

        assertThrows(PlaylistProtocolException::class.java) {
            accumulator.append(
                requestedCursor = "cursor-3",
                page = page(trackCount = 3, entries = listOf(entry("entry-3", 2)), nextCursor = "cursor-2"),
            )
        }
    }

    private fun page(
        trackCount: Int,
        entries: List<PlaylistEntryDto>,
        nextCursor: String?,
    ) = PlaylistDetailDto(
        id = PLAYLIST_ID,
        owner = USER,
        name = "Long playlist",
        description = null,
        visibility = "PRIVATE",
        cover = null,
        trackCount = trackCount,
        version = 1,
        createdAt = TIMESTAMP,
        updatedAt = TIMESTAMP,
        entries = entries,
        nextCursor = nextCursor,
    )

    private fun entry(id: String, position: Int) = PlaylistEntryDto(
        id = id,
        position = position,
        track =
        TrackSummaryDto(
            id = "track-$position",
            title = "Track $position",
            artists = listOf(ArtistReferenceDto("artist-1", "Artist")),
            album = null,
            artwork = null,
            durationMs = 180_000,
            trackNumber = position + 1,
            discNumber = 1,
            isFavorite = false,
            publishedAt = TIMESTAMP,
        ),
        addedBy = USER,
        addedAt = TIMESTAMP,
    )

    private companion object {
        const val PLAYLIST_ID = "playlist-1"
        const val TIMESTAMP = "2026-01-01T00:00:00Z"
        val USER = UserSummaryDto("user-1", "user", "User", null)
    }
}
