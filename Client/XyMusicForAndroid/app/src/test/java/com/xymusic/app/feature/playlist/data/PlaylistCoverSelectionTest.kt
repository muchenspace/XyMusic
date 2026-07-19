package com.xymusic.app.feature.playlist.data

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.database.entity.PlaylistEntryEntity
import org.junit.Test

class PlaylistCoverSelectionTest {
    @Test
    fun latestAddedEntryProvidesPlaylistCoverTrack() {
        val entries =
            listOf(
                entry(id = "entry-old", trackId = "track-old", addedAt = 1_000),
                entry(id = "entry-latest", trackId = "track-latest", addedAt = 2_000),
            )

        assertThat(latestCoverTrackId(entries)).isEqualTo("track-latest")
    }

    @Test
    fun entryIdBreaksEqualTimestampTiesLikeServerOrdering() {
        val entries =
            listOf(
                entry(id = "entry-a", trackId = "track-a", addedAt = 2_000),
                entry(id = "entry-b", trackId = "track-b", addedAt = 2_000),
            )

        assertThat(latestCoverTrackId(entries)).isEqualTo("track-b")
    }

    @Test
    fun emptyPlaylistHasNoCoverTrack() {
        assertThat(latestCoverTrackId(emptyList())).isNull()
    }

    private fun entry(id: String, trackId: String, addedAt: Long) = PlaylistEntryEntity(
        ownerUserId = "owner",
        id = id,
        playlistId = "playlist",
        position = 0,
        trackId = trackId,
        addedByUserId = "owner",
        addedAtEpochMs = addedAt,
    )
}
