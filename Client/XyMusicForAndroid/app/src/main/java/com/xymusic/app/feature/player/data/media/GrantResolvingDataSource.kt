package com.xymusic.app.feature.player.data.media

import android.net.Uri
import androidx.media3.common.C
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.DataSpec
import androidx.media3.datasource.HttpDataSource
import androidx.media3.datasource.TransferListener
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import com.xymusic.app.feature.player.domain.PlayerResult
import java.io.IOException
import kotlinx.coroutines.runBlocking

@UnstableApi
class GrantResolvingDataSourceFactory(
    private val onlineFactory: DataSource.Factory,
    private val offlineFactory: DataSource.Factory,
    private val grantRepository: PlaybackGrantRepository,
    private val offlineMediaStore: OfflineMediaStore,
    private val sessionProvider: AppSessionProvider,
    private val sessionIdentityProvider: SessionIdentityProvider,
    private val sessionMutationCoordinator: SessionMutationCoordinator,
) : DataSource.Factory {
    override fun createDataSource(): DataSource = GrantResolvingDataSource(
        onlineFactory = onlineFactory,
        offlineFactory = offlineFactory,
        grantRepository = grantRepository,
        offlineMediaStore = offlineMediaStore,
        sessionProvider = sessionProvider,
        sessionIdentityProvider = sessionIdentityProvider,
        sessionMutationCoordinator = sessionMutationCoordinator,
    )
}

@UnstableApi
private class GrantResolvingDataSource(
    private val onlineFactory: DataSource.Factory,
    private val offlineFactory: DataSource.Factory,
    private val grantRepository: PlaybackGrantRepository,
    private val offlineMediaStore: OfflineMediaStore,
    private val sessionProvider: AppSessionProvider,
    private val sessionIdentityProvider: SessionIdentityProvider,
    private val sessionMutationCoordinator: SessionMutationCoordinator,
) : DataSource {
    private val transferListeners = mutableListOf<TransferListener>()
    private var upstream: DataSource? = null
    private var stableUri: Uri? = null
    private var boundIdentity: ActiveSessionIdentity? = null
    private var openedAtEndOfInput = false

    override fun addTransferListener(transferListener: TransferListener) {
        transferListeners += transferListener
        upstream?.addTransferListener(transferListener)
    }

    override fun open(dataSpec: DataSpec): Long {
        stableUri = dataSpec.uri
        openedAtEndOfInput = false
        val identity =
            sessionIdentityProvider.activeIdentity()
                ?: throw IOException("Playback session is unavailable")
        boundIdentity = identity
        return try {
            openForIdentity(dataSpec, identity)
        } catch (failure: Exception) {
            closeUpstream()
            boundIdentity = null
            openedAtEndOfInput = false
            throw failure
        }
    }

    private fun openForIdentity(dataSpec: DataSpec, identity: ActiveSessionIdentity): Long {
        val trackId = playbackTrackId(dataSpec.uri)
        val offlineResult =
            if (grantRepository.isCompatibleCodecFallbackEnabled(trackId)) {
                null
            } else {
                openOfflineTrack(dataSpec, identity, trackId)
            }
        if (offlineResult != null) return offlineResult

        var lastFailure: IOException? = null
        repeat(MAX_GRANT_ATTEMPTS) { attempt ->
            requireCurrentIdentity(identity)
            closeUpstream()
            val grant = playbackGrant(trackId, forceRefresh = attempt > 0)
            requireCurrentIdentity(identity)
            val resolvedSpec =
                dataSpec
                    .buildUpon()
                    .setUri(grant.signedUrl)
                    .setKey(grant.cacheKey)
                    .build()
            try {
                return openUpstreamForIdentity(resolvedSpec, identity, onlineFactory)
            } catch (failure: IOException) {
                lastFailure = failure
                if (!failure.isExpiredGrantResponse() || attempt == MAX_GRANT_ATTEMPTS - 1) {
                    throw failure
                }
                requireCurrentIdentity(identity)
                grantRepository.invalidate(trackId)
            }
        }
        throw checkNotNull(lastFailure)
    }

    private fun playbackTrackId(uri: Uri): String = try {
        PlaybackMediaUri.trackId(uri)
    } catch (failure: Exception) {
        throw IOException("Invalid playback media identifier", failure)
    }

    private fun openOfflineTrack(dataSpec: DataSpec, identity: ActiveSessionIdentity, trackId: String): Long? =
        runBlocking {
            sessionMutationCoordinator.mutate {
                requireCurrentIdentity(identity)
                val ownerUserId =
                    (sessionProvider.sessionState.value as? AppSessionState.SignedIn)?.userId
                        ?: throw IOException("Playback session is unavailable")
                if (ownerUserId != identity.userId) {
                    throw IOException("Playback session changed")
                }
                val offlineTrack =
                    offlineMediaStore.playableTrack(ownerUserId, trackId)
                        ?: return@mutate null
                val remainingLength = offlineTrack.contentLength - dataSpec.position
                if (remainingLength <= 0) {
                    openedAtEndOfInput = true
                    return@mutate 0L
                }
                val boundedLength =
                    if (dataSpec.length == C.LENGTH_UNSET.toLong()) {
                        remainingLength
                    } else {
                        minOf(dataSpec.length, remainingLength)
                    }
                if (boundedLength <= 0) {
                    throw IOException("Offline playback length is invalid")
                }
                openUpstreamForIdentity(
                    dataSpec
                        .buildUpon()
                        .setKey(offlineTrack.cacheKey)
                        .setLength(boundedLength)
                        .build(),
                    identity,
                    offlineFactory,
                )
            }
        }

    private fun playbackGrant(trackId: String, forceRefresh: Boolean) = when (
        val result =
            runBlocking {
                grantRepository.get(trackId = trackId, forceRefresh = forceRefresh)
            }
    ) {
        is PlayerResult.Success -> result.value
        is PlayerResult.Failure -> throw IOException("Playback grant is unavailable")
    }

    override fun read(buffer: ByteArray, offset: Int, length: Int): Int {
        requireCurrentIdentity(
            boundIdentity ?: throw IOException("Playback data source is not open"),
        )
        if (length == 0) return 0
        if (openedAtEndOfInput) return C.RESULT_END_OF_INPUT
        return requireNotNull(upstream).read(buffer, offset, length)
    }

    override fun getUri(): Uri? = stableUri

    override fun getResponseHeaders(): Map<String, List<String>> = emptyMap()

    override fun close() {
        closeUpstream()
        stableUri = null
        boundIdentity = null
        openedAtEndOfInput = false
    }

    private fun closeUpstream() {
        runCatching { upstream?.close() }
        upstream = null
    }

    private fun openUpstream(dataSpec: DataSpec, factory: DataSource.Factory): Long {
        val candidate =
            factory.createDataSource().also { source ->
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

    private fun IOException.isExpiredGrantResponse(): Boolean = generateSequence<Throwable>(this) {
        it.cause
    }.filterIsInstance<HttpDataSource.InvalidResponseCodeException>()
        .any { it.responseCode == 401 || it.responseCode == 403 }

    private companion object {
        const val MAX_GRANT_ATTEMPTS = 3
    }
}
