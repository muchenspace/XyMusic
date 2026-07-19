package com.xymusic.app.core.database.model

import com.xymusic.app.core.database.entity.ArtworkColumns
import com.xymusic.app.core.model.media.Artwork

fun ArtworkColumns?.toDomainArtwork(): Artwork? = this?.assetId?.let { assetId ->
    Artwork(
        assetId = assetId,
        url = requireNotNull(url),
        cacheKey = requireNotNull(cacheKey),
        mimeType = requireNotNull(mimeType),
        expiresAtEpochMillis = expiresAtEpochMs,
        width = width,
        height = height,
    )
}
