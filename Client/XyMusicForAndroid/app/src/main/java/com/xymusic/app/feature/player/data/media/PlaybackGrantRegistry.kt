package com.xymusic.app.feature.player.data.media

import android.net.Uri
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.feature.player.domain.PlaybackGrant
import com.xymusic.app.feature.player.domain.PlaybackStreamProtocol
import java.util.UUID
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Binds server-issued playback URLs to the identity and cache key that issued
 * them. Media3 opens the playlist and every child segment independently, so
 * those requests must be authorized locally without resolving the track grant
 * again for each child.
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
        if (request.sessionId != grant.sessionId) return false
        val ticket = streamUri.getQueryParameter(TICKET_QUERY_PARAMETER)?.takeIf(String::isNotBlank)
            ?: return false
        entries[grant.sessionId] =
            Entry(
                identity = identity,
                sessionId = grant.sessionId,
                protocol = grant.streamProtocol,
                cacheKey = grant.cacheKey,
                streamUri = streamUri,
                ticket = ticket,
        )
        while (entries.size > MAX_ENTRIES) entries.remove(entries.entries.first().key)
        return true
    }

    @Synchronized
    fun resolve(uri: Uri, identity: ActiveSessionIdentity): PlaybackResource? {
        val request = PlaybackRequest.parse(uri) ?: return null
        val entry = entries[request.sessionId] ?: return null
        if (entry.identity != identity || !sameOrigin(uri, entry.streamUri)) return null
        if (uri.getQueryParameter(TICKET_QUERY_PARAMETER) != entry.ticket) return null
        val requestedCacheKey = uri.getQueryParameter(CACHE_KEY_QUERY_PARAMETER)
        if (requestedCacheKey != null && requestedCacheKey != entry.cacheKey) return null

        return when (entry.protocol) {
            PlaybackStreamProtocol.PROGRESSIVE ->
                if (request.kind == PlaybackRequestKind.PROGRESSIVE) {
                    PlaybackResource(uri, entry.cacheKey, PlaybackResourceKind.PROGRESSIVE)
                } else {
                    null
                }
            PlaybackStreamProtocol.HLS ->
                when (request.kind) {
                    PlaybackRequestKind.HLS_PLAYLIST ->
                        PlaybackResource(
                            uri,
                            "${entry.cacheKey}:hls:index.m3u8",
                            PlaybackResourceKind.HLS_PLAYLIST,
                        )
                    PlaybackRequestKind.HLS_SEGMENT ->
                        if (requestedCacheKey == entry.cacheKey) {
                            PlaybackResource(
                                uri,
                                "${entry.cacheKey}:hls:${request.name}",
                                PlaybackResourceKind.HLS_SEGMENT,
                            )
                        } else {
                            null
                        }
                    PlaybackRequestKind.PROGRESSIVE -> null
                }
        }
    }

    @Synchronized
    fun clear() {
        entries.clear()
    }

    fun isPlaybackUri(uri: Uri): Boolean = PlaybackRequest.parse(uri) != null

    private data class Entry(
        val identity: ActiveSessionIdentity,
        val sessionId: String,
        val protocol: PlaybackStreamProtocol,
        val cacheKey: String,
        val streamUri: Uri,
        val ticket: String,
    )

    private enum class PlaybackRequestKind {
        PROGRESSIVE,
        HLS_PLAYLIST,
        HLS_SEGMENT,
    }

    private data class PlaybackRequest(
        val sessionId: String,
        val kind: PlaybackRequestKind,
        val name: String = "",
    ) {
        companion object {
            fun parse(uri: Uri): PlaybackRequest? {
                val pathSegments = uri.pathSegments
                val streamsIndex = pathSegments.indexOfLast { it == STREAMS_PATH_SEGMENT }
                if (streamsIndex < 0 || pathSegments.size <= streamsIndex + 1) return null
                val sessionId = pathSegments[streamsIndex + 1]
                if (runCatching { UUID.fromString(sessionId) }.isFailure) return null
                if (pathSegments.size == streamsIndex + 2) {
                    return PlaybackRequest(sessionId, PlaybackRequestKind.PROGRESSIVE)
                }
                if (
                    pathSegments.size == streamsIndex + 3 &&
                    pathSegments[streamsIndex + 2] == PLAYLIST_NAME
                ) {
                    return PlaybackRequest(sessionId, PlaybackRequestKind.HLS_PLAYLIST)
                }
                if (
                    pathSegments.size == streamsIndex + 4 &&
                    pathSegments[streamsIndex + 2] == HLS_PATH_SEGMENT &&
                    isValidSegmentName(pathSegments[streamsIndex + 3])
                ) {
                    return PlaybackRequest(
                        sessionId,
                        PlaybackRequestKind.HLS_SEGMENT,
                        pathSegments[streamsIndex + 3],
                    )
                }
                return null
            }
        }
    }

    private companion object {
        const val MAX_ENTRIES = 128
        const val LOAD_FACTOR = 0.75f
        const val STREAMS_PATH_SEGMENT = "streams"
        const val HLS_PATH_SEGMENT = "hls"
        const val PLAYLIST_NAME = "index.m3u8"
        const val INIT_SEGMENT_NAME = "init.mp4"
        const val TICKET_QUERY_PARAMETER = "ticket"
        const val CACHE_KEY_QUERY_PARAMETER = "cacheKey"
        val SEGMENT_NAME_REGEX = Regex("segment_[0-9]{6}\\.m4s")

        fun isValidSegmentName(name: String): Boolean =
            name == INIT_SEGMENT_NAME || SEGMENT_NAME_REGEX.matches(name)

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

enum class PlaybackResourceKind {
    PROGRESSIVE,
    HLS_PLAYLIST,
    HLS_SEGMENT,
}

data class PlaybackResource(
    val uri: Uri,
    val cacheKey: String,
    val kind: PlaybackResourceKind,
)
