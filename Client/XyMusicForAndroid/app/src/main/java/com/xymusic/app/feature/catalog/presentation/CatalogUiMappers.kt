package com.xymusic.app.feature.catalog.presentation

import com.xymusic.app.core.model.media.Album
import com.xymusic.app.core.model.media.Artist
import com.xymusic.app.core.ui.media.toUi

internal fun Album.toDetailUi(): CatalogAlbumDetailUi = CatalogAlbumDetailUi(
    album = toUi(),
    description = description,
)

internal fun Artist.toDetailUi(): CatalogArtistDetailUi = CatalogArtistDetailUi(
    artist = toUi(),
    description = description,
)
