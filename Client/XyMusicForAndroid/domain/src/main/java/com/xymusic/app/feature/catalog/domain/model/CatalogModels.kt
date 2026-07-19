package com.xymusic.app.feature.catalog.domain.model

import java.util.UUID

enum class TrackSort {
    PUBLISHED_DESC,
    TITLE_ASC,
    TITLE_DESC,
    ALBUM_ORDER_ASC,
}

enum class ArtistSort {
    NAME_ASC,
    NAME_DESC,
}

enum class AlbumSort {
    RELEASE_DATE_DESC,
    TITLE_ASC,
    TITLE_DESC,
}

data class TrackQuery(
    val artistId: String? = null,
    val albumId: String? = null,
    val sort: TrackSort = TrackSort.PUBLISHED_DESC,
) {
    init {
        artistId?.let { requireUuidFilter(it, "artistId") }
        albumId?.let { requireUuidFilter(it, "albumId") }
        require(sort != TrackSort.ALBUM_ORDER_ASC || albumId != null) {
            "ALBUM_ORDER_ASC requires an albumId filter"
        }
    }
}

data class ArtistQuery(val sort: ArtistSort = ArtistSort.NAME_ASC)

data class AlbumQuery(val artistId: String? = null, val sort: AlbumSort = AlbumSort.RELEASE_DATE_DESC) {
    init {
        artistId?.let { requireUuidFilter(it, "artistId") }
    }
}

private fun requireUuidFilter(value: String, name: String) {
    require(runCatching { UUID.fromString(value) }.isSuccess) { "$name must be a UUID" }
}
