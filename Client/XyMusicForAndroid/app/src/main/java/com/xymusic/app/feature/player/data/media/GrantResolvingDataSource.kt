package com.xymusic.app.feature.player.data.media

import android.net.Uri
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.DataSpec
import androidx.media3.datasource.TransferListener
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.core.session.SessionIdentityProvider
import java.io.IOException

/**
 * Opens only resources that were resolved by GrantResolvingMediaSourceFactory.
 * Grant/network resolution belongs to the suspending media-source preparation
 * path; this adapter stays synchronous and only routes already authorized
 * URLs to the right cache/network data source.
 */
@UnstableApi
class GrantResolvingDataSourceFactory(
    private val onlineFactory: DataSource.Factory,
    private val networkFactory: DataSource.Factory,
    private val offlineFactory: DataSource.Factory,
    private val grantRegistry: PlaybackGrantRegistry,
    private val sessionIdentityProvider: SessionIdentityProvider,
) : DataSource.Factory {
    override fun createDataSource(): DataSource = GrantResolvingDataSource(
        onlineFactory = onlineFactory,
        networkFactory = networkFactory,
        offlineFactory = offlineFactory,
        grantRegistry = grantRegistry,
        sessionIdentityProvider = sessionIdentityProvider,
    )
}

@UnstableApi
private class GrantResolvingDataSource(
    private val onlineFactory: DataSource.Factory,
    private val networkFactory: DataSource.Factory,
    private val offlineFactory: DataSource.Factory,
    private val grantRegistry: PlaybackGrantRegistry,
    private val sessionIdentityProvider: SessionIdentityProvider,
) : DataSource {
    private val transferListeners = mutableListOf<TransferListener>()
    private var upstream: DataSource? = null
    private var stableUri: Uri? = null
    private var boundIdentity: ActiveSessionIdentity? = null

    override fun addTransferListener(transferListener: TransferListener) {
        transferListeners += transferListener
        upstream?.addTransferListener(transferListener)
    }

    override fun open(dataSpec: DataSpec): Long {
        val identity = sessionIdentityProvider.activeIdentity()
            ?: throw IOException("Playback session is unavailable")
        closeUpstream()
        stableUri = dataSpec.uri
        boundIdentity = identity
        return try {
            requireCurrentIdentity(identity)
            val resource = grantRegistry.resolve(dataSpec.uri, identity)
            val offlineCacheKey = PlaybackOfflineUri.cacheKey(dataSpec.uri)
            when {
                resource != null -> openManagedResource(dataSpec, identity, resource)
                offlineCacheKey != null -> openOfflineResource(dataSpec, identity, offlineCacheKey)
                grantRegistry.isPlaybackUri(dataSpec.uri) ->
                    throw IOException("Playback URL authorization is unavailable")
                else -> throw IOException("Unsupported playback media URI")
            }
        } catch (failure: Exception) {
            closeUpstream()
            stableUri = null
            boundIdentity = null
            throw failure
        }
    }

    private fun openManagedResource(
        dataSpec: DataSpec,
        identity: ActiveSessionIdentity,
        resource: PlaybackResource,
    ): Long {
        val factory = when (resource.kind) {
            PlaybackResourceKind.HLS_PLAYLIST -> networkFactory
            PlaybackResourceKind.PROGRESSIVE,
            PlaybackResourceKind.HLS_SEGMENT,
            -> onlineFactory
        }
        return openUpstreamForIdentity(
            dataSpec.buildUpon().setKey(resource.cacheKey).build(),
            identity,
            factory,
        )
    }

    private fun openOfflineResource(
        dataSpec: DataSpec,
        identity: ActiveSessionIdentity,
        cacheKey: String,
    ): Long = openUpstreamForIdentity(
        dataSpec.buildUpon().setKey(cacheKey).build(),
        identity,
        offlineFactory,
    )

    override fun read(buffer: ByteArray, offset: Int, length: Int): Int {
        requireCurrentIdentity(boundIdentity ?: throw IOException("Playback data source is not open"))
        if (length == 0) return 0
        return requireNotNull(upstream).read(buffer, offset, length)
    }

    override fun getUri(): Uri? = stableUri

    override fun getResponseHeaders(): Map<String, List<String>> =
        upstream?.responseHeaders ?: emptyMap()

    override fun close() {
        closeUpstream()
        stableUri = null
        boundIdentity = null
    }

    private fun openUpstream(dataSpec: DataSpec, factory: DataSource.Factory): Long {
        val candidate = factory.createDataSource().also { source ->
            transferListeners.forEach(source::addTransferListener)
        }
        upstream = candidate
        return candidate.open(dataSpec)
    }

    private fun openUpstreamForIdentity(
        dataSpec: DataSpec,
        identity: ActiveSessionIdentity,
        factory: DataSource.Factory,
    ): Long {
        val openedLength = openUpstream(dataSpec, factory)
        try {
            requireCurrentIdentity(identity)
        } catch (failure: IOException) {
            closeUpstream()
            throw failure
        }
        return openedLength
    }

    private fun requireCurrentIdentity(expectedIdentity: ActiveSessionIdentity) {
        if (sessionIdentityProvider.activeIdentity() != expectedIdentity) {
            throw IOException("Playback session changed")
        }
    }

    private fun closeUpstream() {
        runCatching { upstream?.close() }
        upstream = null
    }
}
