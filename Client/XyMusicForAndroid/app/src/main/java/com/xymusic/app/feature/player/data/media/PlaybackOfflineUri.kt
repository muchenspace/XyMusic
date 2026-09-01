package com.xymusic.app.feature.player.data.media

import android.net.Uri

internal object PlaybackOfflineUri {
    private const val SCHEME = "xymusic-offline"
    private const val AUTHORITY = "track"
    private const val CACHE_KEY_PARAMETER = "cacheKey"

    fun forTrack(trackId: String, cacheKey: String): Uri =
        Uri
            .Builder()
            .scheme(SCHEME)
            .authority(AUTHORITY)
            .appendPath(trackId)
            .appendQueryParameter(CACHE_KEY_PARAMETER, cacheKey)
            .build()

    fun cacheKey(uri: Uri): String? {
        if (uri.scheme != SCHEME || uri.authority != AUTHORITY || uri.pathSegments.size != 1) return null
        return uri.getQueryParameter(CACHE_KEY_PARAMETER)?.takeIf(String::isNotBlank)
    }
}
