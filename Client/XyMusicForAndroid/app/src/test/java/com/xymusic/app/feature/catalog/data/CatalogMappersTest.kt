package com.xymusic.app.feature.catalog.data

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.data.media.remote.AlbumReferenceDto
import com.xymusic.app.core.data.media.remote.ArtistReferenceDto
import com.xymusic.app.core.data.media.remote.LyricsResourceDto
import com.xymusic.app.core.data.media.remote.TrackDetailDto
import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import com.xymusic.app.core.data.media.toWriteModel
import com.xymusic.app.core.database.model.LyricsTiming
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
    fun detailLyricsMustBelongToTrack() {
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
                lyric = lyrics().copy(trackId = "55555555-5555-4555-8555-555555555555"),
            )

        val failure = runCatching { detail.toWriteModel(1_000L) }.exceptionOrNull()

        assertThat(failure).isInstanceOf(IllegalArgumentException::class.java)
    }

    @Test
    fun lyricTimingIsMappedAndUnknownValuesAreRejected() {
        val wordDetail =
            detail(lyrics().copy(timing = "WORD", content = "[00:00.00]<00:00.00>Track"))
        val unknownDetail = detail(lyrics().copy(timing = "future-value"))

        assertThat(wordDetail.toWriteModel(1_000L).lyrics!!.single().timing)
            .isEqualTo(LyricsTiming.WORD)
        assertThat(runCatching { unknownDetail.toWriteModel(1_000L) }.exceptionOrNull())
            .isInstanceOf(IllegalArgumentException::class.java)
    }

    @Test
    fun plainLyricsCannotDeclareWordTiming() {
        val invalidDetail = detail(lyrics().copy(format = "PLAIN", timing = "WORD", content = "plain"))

        assertThat(runCatching { invalidDetail.toWriteModel(1_000L) }.exceptionOrNull())
            .isInstanceOf(IllegalArgumentException::class.java)
    }

    @Test
    fun lrcLyricsMustMatchTheirDeclaredTimingBeforeCaching() {
        val invalidLyrics =
            listOf(
                lyrics().copy(timing = "WORD", content = "[00:01.00]line"),
                lyrics().copy(timing = "LINE", content = "[00:01.00]<00:01.00>word"),
                lyrics().copy(
                    timing = "WORD",
                    content = "[00:01.00]<00:01.00>valid<00:60.00invalid",
                ),
            )

        invalidLyrics.forEach { lyric ->
            assertThat(runCatching { detail(lyric).toWriteModel(1_000L) }.exceptionOrNull())
                .isInstanceOf(IllegalArgumentException::class.java)
        }
    }

    private fun detail(lyric: LyricsResourceDto) = TrackDetailDto(
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
        lyric = lyric,
    )

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
        timing = "LINE",
        content = "[00:00.00]Track",
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
