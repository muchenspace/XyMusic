package com.xymusic.app.feature.player.data.media

import com.xymusic.app.core.database.OfflineAccountDataCleaner
import com.xymusic.app.core.database.dao.OfflineTrackDao
import com.xymusic.app.core.database.entity.OfflineTrackEntity
import dagger.Lazy
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

interface OfflineMediaCache {
    fun pin(cacheKey: String)

    fun unpin(cacheKey: String)

    fun promotePin(cacheKey: String)

    fun isFullyCached(cacheKey: String, contentLength: Long): Boolean

    suspend fun remove(cacheKey: String)
}

@Singleton
class OfflineMediaStore
@Inject
constructor(
    private val offlineTrackDao: OfflineTrackDao,
    private val offlineMediaCache: Lazy<OfflineMediaCache>,
) : OfflineAccountDataCleaner {
    private val mutationMutex = Mutex()
    private val activeDownloadClaims = mutableMapOf<String, MutableSet<DownloadClaim>>()

    fun createDownloadClaim(cacheKey: String): DownloadClaim {
        require(cacheKey.isNotBlank()) { "Cache key cannot be blank" }
        return DownloadClaim(cacheKey)
    }

    suspend fun beginDownload(claim: DownloadClaim) = mutationMutex.withLock {
        val claims = activeDownloadClaims.getOrPut(claim.cacheKey) { mutableSetOf() }
        if (claims.isEmpty()) offlineMediaCache.get().pin(claim.cacheKey)
        claims += claim
    }

    suspend fun playableTrack(ownerUserId: String, trackId: String): OfflineTrackEntity? = mutationMutex.withLock {
        val track = offlineTrackDao.track(ownerUserId, trackId) ?: return@withLock null
        if (offlineMediaCache.get().isFullyCached(track.cacheKey, track.contentLength)) {
            track
        } else {
            removeLocked(track)
            null
        }
    }

    suspend fun commit(track: OfflineTrackEntity, claim: DownloadClaim): Boolean = mutationMutex.withLock {
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

    suspend fun remove(ownerUserId: String, trackId: String): Boolean = mutationMutex.withLock {
        val track = offlineTrackDao.track(ownerUserId, trackId) ?: return@withLock false
        removeLocked(track)
        true
    }

    suspend fun discardUncommitted(claim: DownloadClaim) = mutationMutex.withLock {
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

    class DownloadClaim internal constructor(internal val cacheKey: String)
}
