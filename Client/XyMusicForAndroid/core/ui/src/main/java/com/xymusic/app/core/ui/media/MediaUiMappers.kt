package com.xymusic.app.core.ui.media

import com.xymusic.app.core.model.media.Album
import com.xymusic.app.core.model.media.AlbumReference
import com.xymusic.app.core.model.media.Artist
import com.xymusic.app.core.model.media.ArtistReference
import com.xymusic.app.core.model.media.Artwork
import com.xymusic.app.core.model.media.Track
import java.time.Instant
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale

fun Artwork.toUi(): CatalogArtworkUi = CatalogArtworkUi(
    url = url,
    cacheKey = cacheKey,
)

fun ArtistReference.toUi(): CatalogArtistLinkUi = CatalogArtistLinkUi(
    id = id,
    name = name,
)

fun AlbumReference.toUi(): CatalogAlbumLinkUi = CatalogAlbumLinkUi(
    id = id,
    title = title,
)

fun Track.toUi(): CatalogTrackUi = CatalogTrackUi(
    id = id,
    title = title,
    artists = artists.map { it.toUi() },
    album = album?.toUi(),
    artwork = artwork?.toUi(),
    durationMs = durationMs,
    discNumber = discNumber,
    trackNumber = trackNumber,
)

fun Album.toUi(): CatalogAlbumUi = CatalogAlbumUi(
    id = id,
    title = title,
    artists = artists.map { it.toUi() },
    cover = cover?.toUi(),
    releaseDate = releaseDateEpochMillis?.let(::formatReleaseDate),
    trackCount = trackCount,
)

fun Artist.toUi(): CatalogArtistUi = CatalogArtistUi(
    id = id,
    name = name,
    artwork = artwork?.toUi(),
)

private fun formatReleaseDate(epochMillis: Long): String = DateTimeFormatter
    .ofLocalizedDate(FormatStyle.MEDIUM)
    .withLocale(Locale.getDefault())
    .format(Instant.ofEpochMilli(epochMillis).atZone(ZoneOffset.UTC).toLocalDate())
