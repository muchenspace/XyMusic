package com.xymusic.app.feature.playlist.presentation

import com.xymusic.app.R
import com.xymusic.app.core.ui.media.PlaylistVisibilityOption
import com.xymusic.app.feature.playlist.domain.model.PlaylistVisibility

internal fun PlaylistVisibility.toPlaylistEditorOption(): PlaylistVisibilityOption = when (this) {
    PlaylistVisibility.PRIVATE -> PlaylistVisibilityOption.PRIVATE
    PlaylistVisibility.UNLISTED -> PlaylistVisibilityOption.UNLISTED
    PlaylistVisibility.PUBLIC -> PlaylistVisibilityOption.PUBLIC
}

internal fun PlaylistVisibilityOption.toPlaylistVisibility(): PlaylistVisibility = when (this) {
    PlaylistVisibilityOption.PRIVATE -> PlaylistVisibility.PRIVATE
    PlaylistVisibilityOption.UNLISTED -> PlaylistVisibility.UNLISTED
    PlaylistVisibilityOption.PUBLIC -> PlaylistVisibility.PUBLIC
}

internal fun PlaylistVisibility.labelRes(): Int = when (this) {
    PlaylistVisibility.PRIVATE -> R.string.playlist_visibility_private
    PlaylistVisibility.UNLISTED -> R.string.playlist_visibility_unlisted
    PlaylistVisibility.PUBLIC -> R.string.playlist_visibility_public
}
