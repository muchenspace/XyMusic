package com.xymusic.app.feature.catalog.data

import com.xymusic.app.feature.catalog.domain.model.AlbumQuery
import com.xymusic.app.feature.catalog.domain.model.ArtistQuery
import com.xymusic.app.feature.catalog.domain.model.TrackQuery

internal fun TrackQuery.collectionKey(): String = buildString {
    append("tracks|artist=")
    append(artistId.orEmpty())
    append("|album=")
    append(albumId.orEmpty())
    append("|sort=")
    append(sort.name)
}

internal fun ArtistQuery.collectionKey(): String = "artists|sort=${sort.name}"

internal fun AlbumQuery.collectionKey(): String = "albums|artist=${artistId.orEmpty()}|sort=${sort.name}"
