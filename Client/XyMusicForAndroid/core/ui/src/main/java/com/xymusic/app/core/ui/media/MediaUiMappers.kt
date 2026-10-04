package com.xymusic.app.core.ui.media

import com.xymusic.app.core.model.media.Album
import com.xymusic.app.core.model.media.AlbumReference
import com.xymusic.app.core.model.media.Artist
import com.xymusic.app.core.model.media.ArtistReference
import com.xymusic.app.core.model.media.Artwork
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.core.model.player.PlayerQueueItem
import com.xymusic.app.core.model.playlist.PlaylistVisibility
import java.time.Instant
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale
import java.util.UUID

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

fun CatalogTrackUi.toPlayerQueueItem(): PlayerQueueItem = toPlayerQueueItem(UUID.randomUUID().toString())

fun CatalogTrackUi.toPlayerQueueItem(queueItemId: String): PlayerQueueItem = PlayerQueueItem(
    queueItemId = queueItemId,
    trackId = id,
    title = title,
    artistNames = artists.map(CatalogArtistLinkUi::name),
    albumTitle = album?.title,
    artworkUrl = artwork?.url,
    artworkCacheKey = artwork?.cacheKey,
    durationMs = durationMs,
)

fun PlaylistVisibility.toPlaylistEditorOption(): PlaylistVisibilityOption = when (this) {
    PlaylistVisibility.PRIVATE -> PlaylistVisibilityOption.PRIVATE
    PlaylistVisibility.UNLISTED -> PlaylistVisibilityOption.UNLISTED
    PlaylistVisibility.PUBLIC -> PlaylistVisibilityOption.PUBLIC
}

fun PlaylistVisibilityOption.toPlaylistVisibility(): PlaylistVisibility = when (this) {
    PlaylistVisibilityOption.PRIVATE -> PlaylistVisibility.PRIVATE
    PlaylistVisibilityOption.UNLISTED -> PlaylistVisibility.UNLISTED
    PlaylistVisibilityOption.PUBLIC -> PlaylistVisibility.PUBLIC
}

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
