package com.xymusic.app.core.ui.media

import androidx.compose.runtime.Immutable

@Immutable
data class CatalogArtworkUi(val url: String?, val cacheKey: String?)

@Immutable
data class CatalogArtistLinkUi(val id: String, val name: String)

@Immutable
data class CatalogAlbumLinkUi(val id: String, val title: String)

@Immutable
data class CatalogTrackUi(
    val id: String,
    val title: String,
    val artists: List<CatalogArtistLinkUi>,
    val album: CatalogAlbumLinkUi?,
    val artwork: CatalogArtworkUi?,
    val durationMs: Long,
    val discNumber: Int,
    val trackNumber: Int?,
) {
    // Body properties are evaluated once per instance and stay out of equals(),
    // so list rows do not rebuild the joined artist line on every recomposition.
    val artistLine: String = artists.joinToString(separator = " · ", transform = CatalogArtistLinkUi::name)
}

@Immutable
data class CatalogAlbumUi(
    val id: String,
    val title: String,
    val artists: List<CatalogArtistLinkUi>,
    val cover: CatalogArtworkUi?,
    val releaseDate: String?,
    val trackCount: Int,
) {
    val artistLine: String = artists.joinToString(separator = " · ", transform = CatalogArtistLinkUi::name)
}

@Immutable
data class CatalogArtistUi(val id: String, val name: String, val artwork: CatalogArtworkUi?)
