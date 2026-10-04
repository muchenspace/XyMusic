package com.xymusic.app.core.data.media

import com.xymusic.app.core.data.media.remote.ArtworkDto
import com.xymusic.app.core.database.entity.ArtworkColumns
import com.xymusic.app.core.model.media.Artwork
import java.time.Instant
import java.util.UUID

/**
 * Shared artwork mappings for the remote DTOs.
 *
 * [toValidatedArtworkColumns] keeps the catalog write-path validation (UUID asset ID and
 * non-blank text fields) while [toArtworkColumns] and [toDomainArtwork] are plain mappings
 * used by paths that accept the server response as-is.
 */
internal fun ArtworkDto?.toArtworkColumns(): ArtworkColumns? = this?.let { artwork ->
    ArtworkColumns(
        assetId = artwork.assetId,
        url = artwork.url,
        cacheKey = artwork.cacheKey,
        mimeType = artwork.mimeType,
        expiresAtEpochMs = artwork.expiresAt?.let { Instant.parse(it).toEpochMilli() },
        width = artwork.width,
        height = artwork.height,
    )
}

internal fun ArtworkDto?.toValidatedArtworkColumns(): ArtworkColumns? = this?.also { artwork ->
    require(runCatching { UUID.fromString(artwork.assetId) }.isSuccess) { "Invalid artwork asset ID" }
    require(artwork.url.isNotBlank()) { "Artwork URL cannot be blank" }
    require(artwork.cacheKey.isNotBlank()) { "Artwork cache key cannot be blank" }
    require(artwork.mimeType.isNotBlank()) { "Artwork MIME type cannot be blank" }
}.toArtworkColumns()

internal fun ArtworkDto?.toDomainArtwork(): Artwork? = this?.let { artwork ->
    Artwork(
        assetId = artwork.assetId,
        url = artwork.url,
        cacheKey = artwork.cacheKey,
        mimeType = artwork.mimeType,
        expiresAtEpochMillis = artwork.expiresAt?.let { Instant.parse(it) }?.toEpochMilli(),
        width = artwork.width,
        height = artwork.height,
    )
}
