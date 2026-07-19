package com.xymusic.app.feature.player.data.media

import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSpec
import androidx.media3.datasource.cache.CacheDataSource
import androidx.media3.datasource.cache.CacheWriter
import androidx.media3.datasource.okhttp.OkHttpDataSource
import com.xymusic.app.core.common.IoDispatcher
import com.xymusic.app.core.database.dao.CatalogDao
import com.xymusic.app.core.database.dao.OfflineTrackDao
import com.xymusic.app.core.database.entity.ArtistEntity
import com.xymusic.app.core.database.entity.OfflineTrackEntity
import com.xymusic.app.core.database.entity.TrackArtistCreditEntity
import com.xymusic.app.core.database.model.TrackSummaryReadModel
import com.xymusic.app.core.network.MediaHttpClient
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.feature.player.domain.OfflineTrack
import com.xymusic.app.feature.player.domain.OfflineTrackRepository
import com.xymusic.app.feature.player.domain.OfflineTrackResult
import com.xymusic.app.feature.player.domain.PlaybackGrant
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import com.xymusic.app.feature.player.domain.PlayerResult
import java.time.Clock
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.runInterruptible
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import okhttp3.OkHttpClient

fun interface OfflineMediaDownloader {
    suspend fun download(grant: PlaybackGrant)
}

@Singleton
@UnstableApi
class CacheOfflineMediaDownloader
@Inject
constructor(
    private val playbackCache: PlaybackCache,
    @MediaHttpClient private val mediaHttpClient: OkHttpClient,
    private val playbackNetworkPolicy: PlaybackNetworkPolicy,
    @IoDispatcher private val ioDispatcher: CoroutineDispatcher,
) : OfflineMediaDownloader {
    override suspend fun download(grant: PlaybackGrant) = runInterruptible(ioDispatcher) {
        CacheWriter(
            downloadDataSource(),
            DataSpec
                .Builder()
                .setUri(grant.signedUrl)
                .setKey(grant.cacheKey)
                .setLength(grant.contentLength)
                .build(),
            null,
            null,
        ).cache()
    }

    private fun downloadDataSource(): CacheDataSource {
        val upstream =
            PolicyEnforcingDataSourceFactory(
                OkHttpDataSource.Factory(mediaHttpClient),
                playbackNetworkPolicy,
            )
        return CacheDataSource
            .Factory()
            .setCache(playbackCache.cache)
            .setUpstreamDataSourceFactory(upstream)
            .createDataSource()
    }
}

@Singleton
@UnstableApi
@OptIn(ExperimentalCoroutinesApi::class)
class Media3OfflineTrackRepository
@Inject
constructor(
    private val offlineTrackDao: OfflineTrackDao,
    private val catalogDao: CatalogDao,
    private val offlineMediaStore: OfflineMediaStore,
    private val offlineMediaDownloader: OfflineMediaDownloader,
    private val playbackGrantRepository: PlaybackGrantRepository,
    private val json: Json,
    private val clock: Clock,
    private val sessionProvider: AppSessionProvider,
    private val sessionIdentityProvider: SessionIdentityProvider,
    private val sessionMutationCoordinator: SessionMutationCoordinator,
    @IoDispatcher private val ioDispatcher: CoroutineDispatcher,
) : OfflineTrackRepository {
    private val operationMutexes = Array(TRACK_OPERATION_MUTEX_COUNT) { Mutex() }

    override fun observeAll(): Flow<List<OfflineTrack>> = sessionProvider.sessionState
        .flatMapLatest { state ->
            when (state) {
                is AppSessionState.SignedIn ->
                    offlineTrackDao
                        .observeAll(state.userId)
                        .map { tracks -> tracks.map(::toDomain) }
                AppSessionState.Loading, AppSessionState.SignedOut -> flowOf(emptyList())
            }
        }

    override fun observeDownloaded(trackId: String): Flow<Boolean> = sessionProvider.sessionState
        .flatMapLatest { state ->
            when (state) {
                is AppSessionState.SignedIn ->
                    offlineTrackDao.observeDownloaded(state.userId, trackId)
                AppSessionState.Loading, AppSessionState.SignedOut -> flowOf(false)
            }
        }

    override suspend fun download(trackId: String): OfflineTrackResult = withContext(ioDispatcher) {
        val downloadIdentity =
            activeIdentity()
                ?: return@withContext OfflineTrackResult.Unavailable
        val ownerUserId = downloadIdentity.userId
        operationMutex(trackId).withLock {
            downloadLocked(trackId, downloadIdentity, ownerUserId)
        }
    }

    private suspend fun downloadLocked(
        trackId: String,
        downloadIdentity: ActiveSessionIdentity,
        ownerUserId: String,
    ): OfflineTrackResult {
        return try {
            val existingResult = existingDownloadResult(trackId, downloadIdentity, ownerUserId)
            if (existingResult != null) return existingResult
            val prepared = prepareDownload(trackId, downloadIdentity) ?: return OfflineTrackResult.Unavailable
            executeDownload(prepared, downloadIdentity, ownerUserId)
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: Exception) {
            OfflineTrackResult.Unavailable
        }
    }

    private suspend fun existingDownloadResult(
        trackId: String,
        downloadIdentity: ActiveSessionIdentity,
        ownerUserId: String,
    ): OfflineTrackResult? {
        var identityIsCurrent = false
        val playableTrack =
            sessionMutationCoordinator.mutate {
                identityIsCurrent = isCurrent(downloadIdentity)
                if (identityIsCurrent) {
                    offlineMediaStore.playableTrack(ownerUserId, trackId)
                } else {
                    null
                }
            }
        return when {
            !identityIsCurrent -> OfflineTrackResult.Unavailable
            playableTrack != null -> OfflineTrackResult.Success
            else -> null
        }
    }

    private suspend fun prepareDownload(trackId: String, downloadIdentity: ActiveSessionIdentity): PreparedDownload? {
        val metadata = catalogDao.tracks(listOf(trackId)).singleOrNull() ?: return null
        if (!isCurrent(downloadIdentity)) return null
        val grant =
            when (val result = playbackGrantRepository.get(trackId)) {
                is PlayerResult.Success -> result.value
                is PlayerResult.Failure -> return null
            }
        if (grant.contentLength <= 0L || grant.cacheKey.isBlank()) return null
        if (!isCurrent(downloadIdentity)) return null
        return PreparedDownload(metadata, grant)
    }

    private suspend fun executeDownload(
        prepared: PreparedDownload,
        downloadIdentity: ActiveSessionIdentity,
        ownerUserId: String,
    ): OfflineTrackResult {
        val claim = offlineMediaStore.createDownloadClaim(prepared.grant.cacheKey)
        val operationJob = currentCoroutineContext()[Job]
        return try {
            if (!beginDownload(claim, downloadIdentity)) return OfflineTrackResult.Unavailable
            offlineMediaDownloader.download(prepared.grant)
            currentCoroutineContext().ensureActive()
            val track =
                prepared.metadata.toEntity(
                    ownerUserId = ownerUserId,
                    cacheKey = prepared.grant.cacheKey,
                    contentLength = prepared.grant.contentLength,
                    downloadedAtEpochMillis = clock.millis(),
                    json = json,
                )
            if (!commitDownload(track, claim, downloadIdentity, operationJob)) {
                discardUncommitted(claim)
                return OfflineTrackResult.Unavailable
            }
            OfflineTrackResult.Success
        } catch (failure: CancellationException) {
            discardUncommitted(claim)
            throw failure
        } catch (_: Exception) {
            discardUncommitted(claim)
            OfflineTrackResult.Unavailable
        }
    }

    private suspend fun beginDownload(
        claim: OfflineMediaStore.DownloadClaim,
        downloadIdentity: ActiveSessionIdentity,
    ): Boolean = sessionMutationCoordinator.mutate {
        if (isCurrent(downloadIdentity)) {
            offlineMediaStore.beginDownload(claim)
            true
        } else {
            false
        }
    }

    private suspend fun commitDownload(
        track: OfflineTrackEntity,
        claim: OfflineMediaStore.DownloadClaim,
        downloadIdentity: ActiveSessionIdentity,
        operationJob: Job?,
    ): Boolean = withContext(NonCancellable) {
        sessionMutationCoordinator.mutate {
            if (operationJob?.isActive != true || !isCurrent(downloadIdentity)) {
                false
            } else {
                offlineMediaStore.commit(track, claim)
            }
        }
    }

    override suspend fun remove(trackId: String): OfflineTrackResult = withContext(ioDispatcher) {
        val removeIdentity =
            activeIdentity()
                ?: return@withContext OfflineTrackResult.Unavailable
        operationMutex(trackId).withLock {
            try {
                sessionMutationCoordinator.mutate {
                    if (!isCurrent(removeIdentity)) {
                        OfflineTrackResult.Unavailable
                    } else {
                        offlineMediaStore.remove(removeIdentity.userId, trackId)
                        OfflineTrackResult.Success
                    }
                }
            } catch (failure: CancellationException) {
                throw failure
            } catch (_: Exception) {
                OfflineTrackResult.Unavailable
            }
        }
    }

    private fun operationMutex(trackId: String): Mutex =
        operationMutexes[(trackId.hashCode() and Int.MAX_VALUE) % operationMutexes.size]

    private fun activeIdentity(): ActiveSessionIdentity? {
        val identity = sessionIdentityProvider.activeIdentity() ?: return null
        val ownerUserId =
            (sessionProvider.sessionState.value as? AppSessionState.SignedIn)?.userId ?: return null
        return identity.takeIf { it.userId == ownerUserId }
    }

    private fun isCurrent(expectedIdentity: ActiveSessionIdentity): Boolean = activeIdentity() == expectedIdentity

    private suspend fun discardUncommitted(claim: OfflineMediaStore.DownloadClaim) {
        withContext(NonCancellable) {
            runCatching { offlineMediaStore.discardUncommitted(claim) }
        }
    }

    private data class PreparedDownload(val metadata: TrackSummaryReadModel, val grant: PlaybackGrant)

    private fun toDomain(entity: OfflineTrackEntity): OfflineTrack = OfflineTrack(
        trackId = entity.trackId,
        title = entity.title,
        artistNames =
        runCatching {
            json.decodeFromString<List<String>>(entity.artistNamesJson)
        }.getOrDefault(emptyList()),
        albumTitle = entity.albumTitle,
        artworkUrl = entity.artworkUrl,
        artworkCacheKey = entity.artworkCacheKey,
        durationMs = entity.durationMs,
        downloadedAtEpochMillis = entity.downloadedAtEpochMs,
    )
}

private fun TrackSummaryReadModel.toEntity(
    ownerUserId: String,
    cacheKey: String,
    contentLength: Long,
    downloadedAtEpochMillis: Long,
    json: Json,
): OfflineTrackEntity {
    val artistsById = artists.associateBy(ArtistEntity::id)
    val artistNames =
        credits
            .sortedBy(TrackArtistCreditEntity::sortOrder)
            .mapNotNull { credit -> artistsById[credit.artistId]?.name }
            .distinct()
    return OfflineTrackEntity(
        ownerUserId = ownerUserId,
        trackId = track.id,
        title = track.title,
        artistNamesJson = json.encodeToString(artistNames),
        albumTitle = album?.title,
        artworkUrl = track.artwork?.url ?: album?.cover?.url,
        artworkCacheKey = track.artwork?.cacheKey ?: album?.cover?.cacheKey,
        durationMs = track.durationMs,
        cacheKey = cacheKey,
        contentLength = contentLength,
        downloadedAtEpochMs = downloadedAtEpochMillis,
    )
}

private const val TRACK_OPERATION_MUTEX_COUNT = 32
