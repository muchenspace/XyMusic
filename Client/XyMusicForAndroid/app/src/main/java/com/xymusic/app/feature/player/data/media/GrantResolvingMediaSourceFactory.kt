package com.xymusic.app.feature.player.data.media

import android.net.Uri
import android.os.Handler
import android.os.Looper
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.MimeTypes
import androidx.media3.common.Timeline
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.TransferListener
import androidx.media3.exoplayer.drm.DrmSessionManagerProvider
import androidx.media3.exoplayer.hls.HlsMediaSource
import androidx.media3.exoplayer.source.CompositeMediaSource
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import androidx.media3.exoplayer.source.ForwardingTimeline
import androidx.media3.exoplayer.source.MediaPeriod
import androidx.media3.exoplayer.source.MediaSource
import androidx.media3.exoplayer.upstream.Allocator
import androidx.media3.exoplayer.upstream.LoadErrorHandlingPolicy
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.feature.player.adapter.media3.PlaybackMediaUri
import com.xymusic.app.feature.player.adapter.media3.playbackRequestedStartPositionMs
import com.xymusic.app.feature.player.adapter.media3.playbackSourceOffsetMs
import com.xymusic.app.feature.player.adapter.media3.playbackStreamProtocol
import com.xymusic.app.feature.player.adapter.media3.withPlaybackResolution
import com.xymusic.app.feature.player.domain.PlaybackGrant
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import com.xymusic.app.feature.player.domain.PlaybackStreamProtocol
import com.xymusic.app.feature.player.domain.PlayerResult
import java.io.IOException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch

/**
 * Resolves one grant while the Media3 source is prepared, then builds the
 * actual child source from the grant URL. Playlist and segment loads therefore
 * never see the original xymusic:// track URI and never request the grant a
 * second time.
 */
@Singleton
@UnstableApi
class GrantResolvingMediaSourceFactory
@Inject
constructor(
    dataSourceFactory: DataSource.Factory,
    private val grantRepository: PlaybackGrantRepository,
    private val grantRegistry: PlaybackGrantRegistry,
    private val offlineMediaStore: OfflineMediaStore,
    private val sessionProvider: AppSessionProvider,
    private val sessionIdentityProvider: SessionIdentityProvider,
    private val sessionMutationCoordinator: SessionMutationCoordinator,
) : MediaSource.Factory {
    private val progressiveFactory = DefaultMediaSourceFactory(dataSourceFactory)
    private val hlsFactory = HlsMediaSource.Factory(dataSourceFactory)

    override fun createMediaSource(mediaItem: MediaItem): MediaSource {
        val trackId = mediaItem.localConfiguration
            ?.uri
            ?.let { uri -> runCatching { PlaybackMediaUri.trackId(uri) }.getOrNull() }
        if (trackId == null) return progressiveFactory.createMediaSource(mediaItem)

        return GrantResolvingMediaSource(
            mediaItem = mediaItem,
            trackId = trackId,
            grantRepository = grantRepository,
            grantRegistry = grantRegistry,
            offlineMediaStore = offlineMediaStore,
            sessionProvider = sessionProvider,
            sessionIdentityProvider = sessionIdentityProvider,
            sessionMutationCoordinator = sessionMutationCoordinator,
            createProgressiveSource = { resolvedItem ->
                progressiveFactory.createMediaSource(resolvedItem)
            },
            createHlsSource = { resolvedItem ->
                hlsFactory.createMediaSource(resolvedItem)
            },
        )
    }

    override fun getSupportedTypes(): IntArray =
        intArrayOf(C.CONTENT_TYPE_OTHER, C.CONTENT_TYPE_HLS)

    override fun setDrmSessionManagerProvider(provider: DrmSessionManagerProvider): MediaSource.Factory {
        progressiveFactory.setDrmSessionManagerProvider(provider)
        hlsFactory.setDrmSessionManagerProvider(provider)
        return this
    }

    override fun setLoadErrorHandlingPolicy(policy: LoadErrorHandlingPolicy): MediaSource.Factory {
        progressiveFactory.setLoadErrorHandlingPolicy(policy)
        hlsFactory.setLoadErrorHandlingPolicy(policy)
        return this
    }
}

@UnstableApi
private class GrantResolvingMediaSource(
    private val mediaItem: MediaItem,
    private val trackId: String,
    private val grantRepository: PlaybackGrantRepository,
    private val grantRegistry: PlaybackGrantRegistry,
    private val offlineMediaStore: OfflineMediaStore,
    private val sessionProvider: AppSessionProvider,
    private val sessionIdentityProvider: SessionIdentityProvider,
    private val sessionMutationCoordinator: SessionMutationCoordinator,
    private val createProgressiveSource: (MediaItem) -> MediaSource,
    private val createHlsSource: (MediaItem) -> MediaSource,
) : CompositeMediaSource<Unit>() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private var resolutionJob: Job? = null
    private var resolvedChild: MediaSource? = null
    private var resolvedPublishedMediaItem: MediaItem? = null
    private var resolvedHls = false
    private var resolutionFailure: IOException? = null
    private var released = false

    override fun getMediaItem(): MediaItem = mediaItem

    override fun isSingleWindow(): Boolean = true

    override fun prepareSourceInternal(mediaTransferListener: TransferListener?) {
        super.prepareSourceInternal(mediaTransferListener)
        val handler = Handler(Looper.getMainLooper())
        resolutionJob = scope.launch {
            try {
                val resolved = resolveSource()
                handler.post {
                    if (released) return@post
                    val child = when (resolved.protocol) {
                        PlaybackStreamProtocol.HLS -> createHlsSource(resolved.mediaItem)
                        PlaybackStreamProtocol.PROGRESSIVE -> createProgressiveSource(resolved.mediaItem)
                    }
                    resolvedChild = child
                    resolvedPublishedMediaItem = resolved.publishedMediaItem
                    resolvedHls = resolved.protocol == PlaybackStreamProtocol.HLS
                    prepareChildSource(CHILD_SOURCE_ID, child)
                }
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Exception) {
                handler.post {
                    if (released) return@post
                    resolutionFailure = failure.asPlaybackIOException()
                    refreshSourceInfo(Timeline.EMPTY)
                }
            }
        }
    }

    override fun maybeThrowSourceInfoRefreshError() {
        resolutionFailure?.let { throw it }
        super.maybeThrowSourceInfoRefreshError()
    }

    override fun createPeriod(
        id: MediaSource.MediaPeriodId,
        allocator: Allocator,
        startPositionUs: Long,
    ): MediaPeriod {
        val child = checkNotNull(resolvedChild) { "Playback source has not been resolved" }
        return child.createPeriod(
            getMediaPeriodIdForChildMediaPeriodId(CHILD_SOURCE_ID, id) ?: id,
            allocator,
            if (resolvedHls) {
                0
            } else {
                getMediaTimeForChildMediaTime(CHILD_SOURCE_ID, startPositionUs, id)
            },
        )
    }

    override fun releasePeriod(mediaPeriod: MediaPeriod) {
        checkNotNull(resolvedChild) { "Playback source has not been resolved" }.releasePeriod(mediaPeriod)
    }

    override fun onChildSourceInfoRefreshed(
        id: Unit,
        mediaSource: MediaSource,
        timeline: Timeline,
    ) {
        refreshSourceInfo(
            resolvedPublishedMediaItem?.let(timeline::withMediaItem) ?: timeline,
        )
    }

    override fun releaseSourceInternal() {
        released = true
        resolutionJob?.cancel()
        scope.cancel()
        super.releaseSourceInternal()
    }

    private suspend fun resolveSource(): ResolvedPlaybackSource {
        val identity = sessionIdentityProvider.activeIdentity()
            ?: throw IOException("Playback session is unavailable")
        val ownerUserId = (sessionProvider.sessionState.value as? AppSessionState.SignedIn)?.userId
            ?: throw IOException("Playback session is unavailable")
        if (ownerUserId != identity.userId) throw IOException("Playback session changed")

        val offlineTrack = sessionMutationCoordinator.mutate {
            requireCurrentIdentity(identity)
            offlineMediaStore.playableTrack(ownerUserId, trackId)
        }
        if (offlineTrack != null) {
            return ResolvedPlaybackSource(
                protocol = PlaybackStreamProtocol.PROGRESSIVE,
                mediaItem = mediaItem.withUri(
                    PlaybackOfflineUri.forTrack(trackId, offlineTrack.cacheKey),
                ),
                publishedMediaItem = mediaItem.withPlaybackResolution(
                    protocol = PlaybackStreamProtocol.PROGRESSIVE,
                    sourceOffsetMs = 0,
                ),
            )
        }

        val confirmedProtocol = mediaItem.playbackStreamProtocol()
        val requestedProtocol = confirmedProtocol ?: PlaybackStreamProtocol.HLS
        val requestedStartPositionMs =
            if (confirmedProtocol == PlaybackStreamProtocol.HLS) {
                mediaItem.playbackRequestedStartPositionMs() ?: 0
            } else {
                0
            }
        val grant = when (
            val result = grantRepository.get(
                trackId = trackId,
                acceptedCodecs = acceptedCodecsFor(requestedProtocol),
                streamProtocol = requestedProtocol,
                startPositionMs = requestedStartPositionMs,
            )
        ) {
            is PlayerResult.Success -> result.value
            is PlayerResult.Failure -> throw IOException("Playback grant is unavailable")
        }
        requireCurrentIdentity(identity)
        if (!grantRegistry.register(identity, grant)) {
            throw IOException("Playback grant URL is invalid")
        }
        return ResolvedPlaybackSource(
            protocol = grant.streamProtocol,
            mediaItem = mediaItem
                .withUri(grant.streamUrl.toUri(), grant.mimeType, grant.streamProtocol)
                .withPlaybackResolution(
                    protocol = grant.streamProtocol,
                    sourceOffsetMs = 0,
                    requestedStartPositionMs = grant.startPositionMs.takeIf { grant.streamProtocol == PlaybackStreamProtocol.HLS },
                ),
            publishedMediaItem = mediaItem.withPlaybackResolution(
                protocol = grant.streamProtocol,
                sourceOffsetMs = grant.startPositionMs.takeIf { grant.streamProtocol == PlaybackStreamProtocol.HLS } ?: 0,
                requestedStartPositionMs = grant.startPositionMs.takeIf { grant.streamProtocol == PlaybackStreamProtocol.HLS },
            ),
        )
    }

    private fun requireCurrentIdentity(expectedIdentity: ActiveSessionIdentity) {
        if (sessionIdentityProvider.activeIdentity() != expectedIdentity) {
            throw IOException("Playback session changed")
        }
    }

    private fun Exception.asPlaybackIOException(): IOException =
        this as? IOException ?: IOException("Playback source resolution failed", this)

    private data class ResolvedPlaybackSource(
        val protocol: PlaybackStreamProtocol,
        val mediaItem: MediaItem,
        val publishedMediaItem: MediaItem,
    )

    private companion object {
        val CHILD_SOURCE_ID = Unit
        val HLS_STREAM_CODECS = listOf("aac")
        val PROGRESSIVE_STREAM_CODECS = listOf("aac", "mp3", "opus", "flac", "wav")
    }

    private fun acceptedCodecsFor(protocol: PlaybackStreamProtocol): List<String> = when (protocol) {
        PlaybackStreamProtocol.HLS -> HLS_STREAM_CODECS
        PlaybackStreamProtocol.PROGRESSIVE -> PROGRESSIVE_STREAM_CODECS
    }
}

private fun MediaItem.withUri(
    uri: Uri,
    mimeType: String? = null,
    protocol: PlaybackStreamProtocol? = null,
): MediaItem = buildUpon()
    .setUri(uri)
    .apply {
        when (protocol) {
            PlaybackStreamProtocol.HLS -> setMimeType(MimeTypes.APPLICATION_M3U8)
            PlaybackStreamProtocol.PROGRESSIVE -> mimeType?.takeIf(String::isNotBlank)?.let(::setMimeType)
            null -> Unit
        }
    }
    .build()

private fun String.toUri(): Uri = Uri.parse(this)

private fun Timeline.withMediaItem(mediaItem: MediaItem): Timeline = object : ForwardingTimeline(this) {
    override fun getWindow(
        windowIndex: Int,
        window: Timeline.Window,
        defaultPositionProjectionUs: Long,
    ): Timeline.Window = super.getWindow(windowIndex, window, defaultPositionProjectionUs).also {
        it.mediaItem = mediaItem
    }
}
