package com.xymusic.app.core.model.media

data class Artwork(
    val assetId: String,
    val url: String,
    val cacheKey: String,
    val mimeType: String,
    val expiresAtEpochMillis: Long?,
    val width: Int?,
    val height: Int?,
)

data class Artist(val id: String, val name: String, val artwork: Artwork?, val description: String?)

data class ArtistReference(val id: String, val name: String)

data class Album(
    val id: String,
    val title: String,
    val artists: List<ArtistReference>,
    val cover: Artwork?,
    val releaseDateEpochMillis: Long?,
    val trackCount: Int,
    val description: String?,
)

data class AlbumReference(val id: String, val title: String)

data class Track(
    val id: String,
    val title: String,
    val artists: List<ArtistReference>,
    val album: AlbumReference?,
    val artwork: Artwork?,
    val durationMs: Long,
    val trackNumber: Int?,
    val discNumber: Int,
    val publishedAtEpochMillis: Long,
)

data class Lyrics(
    val id: String,
    val trackId: String,
    val language: String,
    val format: LyricsFormat,
    val content: String,
    val isDefault: Boolean,
    val trackVersion: Long,
    val updatedAtEpochMillis: Long,
)

data class TrackDetail(val track: Track, val lyrics: List<Lyrics>)

enum class LyricsFormat {
    LRC,
    PLAIN,
}
