package com.xymusic.app.feature.playlist.presentation

import com.xymusic.app.R
import com.xymusic.app.feature.playlist.domain.model.PlaylistVisibility

internal fun PlaylistVisibility.labelRes(): Int = when (this) {
    PlaylistVisibility.PRIVATE -> R.string.playlist_visibility_private
    PlaylistVisibility.UNLISTED -> R.string.playlist_visibility_unlisted
    PlaylistVisibility.PUBLIC -> R.string.playlist_visibility_public
}
