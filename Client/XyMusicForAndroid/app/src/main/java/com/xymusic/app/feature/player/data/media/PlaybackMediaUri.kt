package com.xymusic.app.feature.player.data.media

import android.net.Uri
import java.util.UUID

object PlaybackMediaUri {
    fun forTrack(trackId: String): Uri {
        UUID.fromString(trackId)
        return Uri
            .Builder()
            .scheme(SCHEME)
            .authority(AUTHORITY)
            .appendPath(trackId)
            .build()
    }

    fun trackId(uri: Uri): String {
        require(uri.scheme == SCHEME && uri.authority == AUTHORITY)
        val trackId = requireNotNull(uri.pathSegments.singleOrNull())
        UUID.fromString(trackId)
        return trackId
    }

    private const val SCHEME = "xymusic"
    private const val AUTHORITY = "track"
}
