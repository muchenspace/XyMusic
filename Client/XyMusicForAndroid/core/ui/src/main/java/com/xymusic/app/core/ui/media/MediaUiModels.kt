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
)

@Immutable
data class CatalogAlbumUi(
    val id: String,
    val title: String,
    val artists: List<CatalogArtistLinkUi>,
    val cover: CatalogArtworkUi?,
    val releaseDate: String?,
    val trackCount: Int,
)

@Immutable
data class CatalogArtistUi(val id: String, val name: String, val artwork: CatalogArtworkUi?)
