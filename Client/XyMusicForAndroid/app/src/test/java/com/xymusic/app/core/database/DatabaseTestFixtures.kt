package com.xymusic.app.core.database

import com.xymusic.app.core.database.entity.ArtistEntity
import com.xymusic.app.core.database.entity.LyricsEntity
import com.xymusic.app.core.database.entity.TrackArtistCreditEntity
import com.xymusic.app.core.database.entity.TrackEntity
import com.xymusic.app.core.database.model.ArtistCreditRole
import com.xymusic.app.core.database.model.LyricsFormat

internal suspend fun XyMusicDatabase.seedTrack(
    trackId: String,
    artistId: String = "artist-$trackId",
    title: String = "Track $trackId",
) {
    catalogDao().upsertArtists(
        listOf(
            ArtistEntity(
                id = artistId,
                name = "Artist $trackId",
                description = null,
                artwork = null,
                cachedAtEpochMs = 1_000,
            ),
        ),
    )
    catalogDao().replaceTrackMetadata(
        track =
        TrackEntity(
            id = trackId,
            albumId = null,
            title = title,
            durationMs = 180_000,
            trackNumber = 1,
            discNumber = 1,
            publishedAtEpochMs = 1_000,
            artwork = null,
            cachedAtEpochMs = 1_000,
        ),
        credits =
        listOf(
            TrackArtistCreditEntity(
                trackId = trackId,
                artistId = artistId,
                role = ArtistCreditRole.PRIMARY,
                sortOrder = 0,
            ),
        ),
    )
    catalogDao().replaceLyrics(
        trackId = trackId,
        lyrics =
        listOf(
            LyricsEntity(
                id = "lyrics-$trackId",
                trackId = trackId,
                language = "zh-CN",
                format = LyricsFormat.LRC,
                content = "[00:00.00]Track $trackId",
                isDefault = true,
                trackVersion = 1,
                updatedAtEpochMs = 1_000,
            ),
        ),
    )
}
