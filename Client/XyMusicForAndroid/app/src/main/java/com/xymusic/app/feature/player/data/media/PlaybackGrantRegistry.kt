package com.xymusic.app.feature.player.data.media

import android.net.Uri
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.feature.player.domain.PlaybackGrant
import java.util.UUID
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Binds server-issued playback URLs to the identity that issued them.
 */
@Singleton
class PlaybackGrantRegistry
@Inject
constructor() {
    private val entries = LinkedHashMap<String, Entry>(MAX_ENTRIES, LOAD_FACTOR, true)

    @Synchronized
    fun register(identity: ActiveSessionIdentity, grant: PlaybackGrant): Boolean {
        val streamUri = Uri.parse(grant.streamUrl)
        val request = PlaybackRequest.parse(streamUri) ?: return false
        if (request.trackId != grant.trackId) return false
        val ticket = streamUri.getQueryParameter(TICKET_QUERY_PARAMETER)?.takeIf(String::isNotBlank)
            ?: return false
        entries[grant.trackId] =
            Entry(
                identity = identity,
                trackId = grant.trackId,
                streamUri = streamUri,
                ticket = ticket,
            )
        while (entries.size > MAX_ENTRIES) entries.remove(entries.entries.first().key)
        return true
    }

    @Synchronized
    fun resolve(uri: Uri, identity: ActiveSessionIdentity): PlaybackResource? {
        val request = PlaybackRequest.parse(uri) ?: return null
        val entry = entries[request.trackId] ?: return null
        if (entry.identity != identity || !sameOrigin(uri, entry.streamUri)) return null
        if (uri.getQueryParameter(TICKET_QUERY_PARAMETER) != entry.ticket) return null

        return PlaybackResource(uri, "track:${entry.trackId}")
    }

    @Synchronized
    fun clear() {
        entries.clear()
    }

    fun isPlaybackUri(uri: Uri): Boolean = PlaybackRequest.parse(uri) != null

    private data class Entry(
        val identity: ActiveSessionIdentity,
        val trackId: String,
        val streamUri: Uri,
        val ticket: String,
    )

    private data class PlaybackRequest(
        val trackId: String,
    ) {
        companion object {
            fun parse(uri: Uri): PlaybackRequest? {
                val pathSegments = uri.pathSegments
                val streamsIndex = pathSegments.indexOfLast { it == STREAMS_PATH_SEGMENT }
                if (streamsIndex < 0 || pathSegments.size != streamsIndex + 2) return null
                val trackId = pathSegments[streamsIndex + 1]
                if (runCatching { UUID.fromString(trackId) }.isFailure) return null
                return PlaybackRequest(trackId)
            }
        }
    }

    private companion object {
        const val MAX_ENTRIES = 128
        const val LOAD_FACTOR = 0.75f
        const val STREAMS_PATH_SEGMENT = "streams"
        const val TICKET_QUERY_PARAMETER = "ticket"

        fun sameOrigin(left: Uri, right: Uri): Boolean =
            left.scheme.equals(right.scheme, ignoreCase = true) &&
                left.host.equals(right.host, ignoreCase = true) &&
                effectivePort(left) == effectivePort(right)

        fun effectivePort(uri: Uri): Int = when {
            uri.port >= 0 -> uri.port
            uri.scheme.equals("https", ignoreCase = true) -> 443
            uri.scheme.equals("http", ignoreCase = true) -> 80
            else -> -1
        }
    }
}

data class PlaybackResource(
    val uri: Uri,
    val cacheKey: String,
)
