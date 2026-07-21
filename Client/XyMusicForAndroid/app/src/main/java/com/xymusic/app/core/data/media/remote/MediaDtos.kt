package com.xymusic.app.core.data.media.remote

import java.io.IOException
import kotlinx.serialization.Serializable

@Serializable
data class RandomCatalogRequestDto(val limit: Int)

@Serializable
data class ArtworkDto(
    val assetId: String,
    val url: String,
    val cacheKey: String,
    val mimeType: String,
    val expiresAt: String? = null,
    val width: Int? = null,
    val height: Int? = null,
)

@Serializable
data class ArtistReferenceDto(val id: String, val name: String)

@Serializable
data class ArtistSummaryDto(val id: String, val name: String, val artwork: ArtworkDto?)

@Serializable
data class ArtistDetailDto(val id: String, val name: String, val artwork: ArtworkDto?, val description: String?)

@Serializable
data class ArtistPageDto(val items: List<ArtistSummaryDto>, val nextCursor: String?)

@Serializable
data class AlbumReferenceDto(val id: String, val title: String)

@Serializable
data class AlbumSummaryDto(
    val id: String,
    val title: String,
    val artists: List<ArtistReferenceDto>,
    val cover: ArtworkDto?,
    val releaseDate: String?,
    val trackCount: Int,
)

@Serializable
data class AlbumDetailDto(
    val id: String,
    val title: String,
    val artists: List<ArtistReferenceDto>,
    val cover: ArtworkDto?,
    val releaseDate: String?,
    val trackCount: Int,
    val description: String?,
)

@Serializable
data class AlbumPageDto(val items: List<AlbumSummaryDto>, val nextCursor: String?)

@Serializable
data class RandomAlbumsResponseDto(val items: List<AlbumSummaryDto>)

@Serializable
data class TrackSummaryDto(
    val id: String,
    val title: String,
    val artists: List<ArtistReferenceDto>,
    val album: AlbumReferenceDto?,
    val artwork: ArtworkDto?,
    val durationMs: Long,
    val trackNumber: Int?,
    val discNumber: Int,
    val isFavorite: Boolean,
    val publishedAt: String,
)

@Serializable
data class LyricsResourceDto(
    val id: String,
    val trackId: String,
    val language: String,
    val format: String,
    val content: String,
    val isDefault: Boolean,
    val trackVersion: Long,
    val updatedAt: String,
)

@Serializable
data class TrackDetailDto(
    val id: String,
    val title: String,
    val artists: List<ArtistReferenceDto>,
    val album: AlbumReferenceDto?,
    val artwork: ArtworkDto?,
    val durationMs: Long,
    val trackNumber: Int?,
    val discNumber: Int,
    val isFavorite: Boolean,
    val publishedAt: String,
    val lyrics: List<LyricsResourceDto>,
    val lyricPage: Int = 1,
    val lyricPageSize: Int = 100,
    val lyricTotal: Int = lyrics.size,
    val lyricTotalPages: Int = if (lyrics.isEmpty()) 0 else 1,
)

@Serializable
data class TrackPageDto(val items: List<TrackSummaryDto>, val nextCursor: String?)

@Serializable
data class RandomTracksResponseDto(val items: List<TrackSummaryDto>)

data class RemotePage<T>(val items: List<T>, val nextCursor: String?)

class CatalogProtocolException(message: String, cause: Throwable? = null) : IOException(message, cause)
