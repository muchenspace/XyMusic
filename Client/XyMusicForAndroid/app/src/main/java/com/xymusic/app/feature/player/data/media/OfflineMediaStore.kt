package com.xymusic.app.feature.player.data.media

import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.cache.Cache
import com.xymusic.app.core.database.OfflineAccountDataCleaner
import com.xymusic.app.core.database.dao.OfflineTrackDao
import com.xymusic.app.core.database.entity.OfflineTrackEntity
import dagger.Lazy
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

@UnstableApi
interface OfflineMediaCache {
    fun pin(cacheKey: String)

    fun unpin(cacheKey: String)

    fun promotePin(cacheKey: String)

    fun isFullyCached(cacheKey: String, contentLength: Long): Boolean

    fun cachedContentLength(cacheKey: String): Long?

    val cache: Cache

    suspend fun remove(cacheKey: String)

    suspend fun clear()
}

interface OfflineMediaStorePort {
    fun createDownloadClaim(cacheKey: String): DownloadClaim

    suspend fun beginDownload(claim: DownloadClaim)

    suspend fun playableTrack(ownerUserId: String, trackId: String): OfflineTrackEntity?

    suspend fun commit(track: OfflineTrackEntity, claim: DownloadClaim): Boolean

    suspend fun remove(ownerUserId: String, trackId: String): Boolean

    suspend fun discardUncommitted(claim: DownloadClaim)
}

class DownloadClaim internal constructor(internal val cacheKey: String)

@Singleton
@UnstableApi
class OfflineMediaStore
@Inject
constructor(
    private val offlineTrackDao: OfflineTrackDao,
    private val offlineMediaCache: Lazy<OfflineMediaCache>,
) : OfflineMediaStorePort,
    OfflineAccountDataCleaner {
    private val mutationMutex = Mutex()
    private val activeDownloadClaims = mutableMapOf<String, MutableSet<DownloadClaim>>()

    override fun createDownloadClaim(cacheKey: String): DownloadClaim {
        require(cacheKey.isNotBlank()) { "Cache key cannot be blank" }
        return DownloadClaim(cacheKey)
    }

    override suspend fun beginDownload(claim: DownloadClaim) = mutationMutex.withLock {
        val claims = activeDownloadClaims.getOrPut(claim.cacheKey) { mutableSetOf() }
        if (claims.isEmpty()) offlineMediaCache.get().pin(claim.cacheKey)
        claims += claim
    }

    override suspend fun playableTrack(ownerUserId: String, trackId: String): OfflineTrackEntity? =
        mutationMutex.withLock {
            val track = offlineTrackDao.track(ownerUserId, trackId) ?: return@withLock null
            if (offlineMediaCache.get().isFullyCached(track.cacheKey, track.contentLength)) {
                track
            } else {
                removeLocked(track)
                null
            }
        }

    override suspend fun commit(track: OfflineTrackEntity, claim: DownloadClaim): Boolean = mutationMutex.withLock {
        require(track.cacheKey == claim.cacheKey) {
            "Download claim belongs to another cache key"
        }
        if (!isActive(claim)) return@withLock false
        if (!offlineMediaCache.get().isFullyCached(track.cacheKey, track.contentLength)) {
            return@withLock false
        }
        offlineTrackDao.upsert(track)
        offlineMediaCache.get().promotePin(track.cacheKey)
        if (releaseDownloadClaim(claim) == 0) {
            offlineMediaCache.get().unpin(track.cacheKey)
        }
        true
    }

    override suspend fun remove(ownerUserId: String, trackId: String): Boolean = mutationMutex.withLock {
        val track = offlineTrackDao.track(ownerUserId, trackId) ?: return@withLock false
        removeLocked(track)
        true
    }

    override suspend fun discardUncommitted(claim: DownloadClaim) = mutationMutex.withLock {
        val remainingClaims = releaseDownloadClaim(claim) ?: return@withLock
        if (remainingClaims > 0) return@withLock
        if (offlineTrackDao.cacheKeyReferenceCount(claim.cacheKey) == 0) {
            offlineMediaCache.get().remove(claim.cacheKey)
        } else {
            offlineMediaCache.get().unpin(claim.cacheKey)
        }
    }

    override suspend fun clear(ownerUserId: String): Int = mutationMutex.withLock {
        require(ownerUserId.isNotBlank()) { "Owner user ID cannot be blank" }
        val ownerTracks = offlineTrackDao.tracks(ownerUserId)
        ownerTracks.groupBy(OfflineTrackEntity::cacheKey).forEach { (cacheKey, tracks) ->
            if (
                cacheKey !in activeDownloadClaims &&
                offlineTrackDao.cacheKeyReferenceCount(cacheKey) == tracks.size
            ) {
                // Remove bytes before metadata so a failed removal remains retryable.
                offlineMediaCache.get().remove(cacheKey)
            }
        }
        offlineTrackDao.deleteOwner(ownerUserId)
    }

    private suspend fun removeLocked(track: OfflineTrackEntity) {
        if (
            track.cacheKey !in activeDownloadClaims &&
            offlineTrackDao.cacheKeyReferenceCount(track.cacheKey) == 1
        ) {
            // Remove bytes before metadata so a failed removal remains retryable.
            offlineMediaCache.get().remove(track.cacheKey)
        }
        offlineTrackDao.delete(track.ownerUserId, track.trackId)
    }

    private fun isActive(claim: DownloadClaim): Boolean = activeDownloadClaims[claim.cacheKey]?.contains(claim) == true

    private fun releaseDownloadClaim(claim: DownloadClaim): Int? {
        val claims = activeDownloadClaims[claim.cacheKey] ?: return null
        if (!claims.remove(claim)) return null
        if (claims.isEmpty()) activeDownloadClaims.remove(claim.cacheKey)
        return claims.size
    }
}
