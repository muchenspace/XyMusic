package com.xymusic.app.feature.catalog.presentation

import androidx.compose.runtime.Immutable
import com.xymusic.app.core.ui.media.CatalogAlbumUi
import com.xymusic.app.core.ui.media.CatalogArtistUi
import com.xymusic.app.core.ui.media.CatalogTrackUi

object CatalogRouteArgs {
    const val AlbumId = "albumId"
    const val ArtistId = "artistId"
}

@Immutable
data class CatalogAlbumDetailUi(val album: CatalogAlbumUi, val description: String?)

@Immutable
data class CatalogArtistDetailUi(val artist: CatalogArtistUi, val description: String?)

enum class ArtistDetailTab {
    Albums,
    Tracks,
}

@Immutable
data class CatalogRandomUiState(
    val featuredAlbums: List<CatalogAlbumUi> = emptyList(),
    val recommendedTracks: List<CatalogTrackUi> = emptyList(),
    val featuredLoading: Boolean = false,
    val recommendedLoading: Boolean = false,
    val featuredFailed: Boolean = false,
    val recommendedFailed: Boolean = false,
)

@Immutable
data class CatalogDetailUiState<T>(
    val item: T? = null,
    val isRefreshing: Boolean = false,
    val refreshFailed: Boolean = false,
)
