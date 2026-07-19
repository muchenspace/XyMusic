package com.xymusic.app.feature.catalog.data

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.data.media.remote.AlbumReferenceDto
import com.xymusic.app.core.data.media.remote.ArtistReferenceDto
import com.xymusic.app.core.data.media.remote.LyricsResourceDto
import com.xymusic.app.core.data.media.remote.TrackDetailDto
import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import com.xymusic.app.core.data.media.toWriteModel
import org.junit.Test

class CatalogMappersTest {
    @Test
    fun trackSummaryCreatesAlbumPlaceholderWithoutLyricsMutation() {
        val write = trackSummary().toWriteModel(cachedAtEpochMs = 1_000L)

        assertThat(write.albumReference?.id).isEqualTo(ALBUM_ID)
        assertThat(write.albumReference?.title).isEqualTo("Album")
        assertThat(write.albumReference?.description).isNull()
        assertThat(write.track.albumId).isEqualTo(ALBUM_ID)
        assertThat(write.lyrics).isNull()
    }

    @Test
    fun duplicateTrackArtistIdsAreRejected() {
        val artist = ArtistReferenceDto(ARTIST_ID, "Artist")
        val failure =
            runCatching {
                trackSummary().copy(artists = listOf(artist, artist)).toWriteModel(1_000L)
            }.exceptionOrNull()

        assertThat(failure).isInstanceOf(IllegalArgumentException::class.java)
    }

    @Test
    fun detailLyricsMustBeUniqueAndBelongToTrack() {
        val lyric = lyrics()
        val detail =
            TrackDetailDto(
                id = TRACK_ID,
                title = "Track",
                artists = listOf(ArtistReferenceDto(ARTIST_ID, "Artist")),
                album = null,
                artwork = null,
                durationMs = 180_000,
                trackNumber = null,
                discNumber = 1,
                isFavorite = false,
                publishedAt = "2026-07-11T00:00:00Z",
                lyrics = listOf(lyric, lyric),
            )

        val failure = runCatching { detail.toWriteModel(1_000L) }.exceptionOrNull()

        assertThat(failure).isInstanceOf(IllegalArgumentException::class.java)
    }

    private fun trackSummary() = TrackSummaryDto(
        id = TRACK_ID,
        title = "Track",
        artists = listOf(ArtistReferenceDto(ARTIST_ID, "Artist")),
        album = AlbumReferenceDto(ALBUM_ID, "Album"),
        artwork = null,
        durationMs = 180_000,
        trackNumber = 1,
        discNumber = 1,
        isFavorite = false,
        publishedAt = "2026-07-11T00:00:00Z",
    )

    private fun lyrics() = LyricsResourceDto(
        id = LYRICS_ID,
        trackId = TRACK_ID,
        language = "zh-CN",
        format = "LRC",
        content = "[00:00.00]Track",
        isDefault = true,
        trackVersion = 1,
        updatedAt = "2026-07-11T00:00:00Z",
    )

    private companion object {
        const val TRACK_ID = "11111111-1111-4111-8111-111111111111"
        const val ARTIST_ID = "22222222-2222-4222-8222-222222222222"
        const val ALBUM_ID = "33333333-3333-4333-8333-333333333333"
        const val LYRICS_ID = "44444444-4444-4444-8444-444444444444"
    }
}
