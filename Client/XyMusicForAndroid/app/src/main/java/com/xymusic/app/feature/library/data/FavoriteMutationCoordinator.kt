package com.xymusic.app.feature.library.data

import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

internal data class FavoriteMutationSnapshot(val revision: Long, val activeTrackIds: Set<String>)

internal class FavoriteMutationCoordinator(stripeCount: Int = DEFAULT_STRIPE_COUNT) {
    private val mutexes: Array<Mutex>
    private val stateLock = Any()
    private var revision = 0L
    private val lastMutationRevisionByTrack = mutableMapOf<String, Long>()
    private val activeTrackIds = mutableSetOf<String>()

    init {
        require(stripeCount > 0) { "Stripe count must be positive" }
        mutexes = Array(stripeCount) { Mutex() }
    }

    suspend fun <T> serialize(trackId: String, operation: suspend () -> T): T {
        val index = (trackId.hashCode() and Int.MAX_VALUE) % mutexes.size
        return mutexes[index].withLock {
            synchronized(stateLock) {
                revision += 1L
                lastMutationRevisionByTrack[trackId] = revision
                activeTrackIds += trackId
            }
            try {
                operation()
            } finally {
                synchronized(stateLock) { activeTrackIds -= trackId }
            }
        }
    }

    fun captureRefreshSnapshot(): FavoriteMutationSnapshot = synchronized(stateLock) {
        FavoriteMutationSnapshot(revision, activeTrackIds.toSet()).also {
            lastMutationRevisionByTrack.clear()
        }
    }

    fun protectedTrackIdsSince(snapshot: FavoriteMutationSnapshot): Set<String> = synchronized(stateLock) {
        buildSet {
            addAll(snapshot.activeTrackIds)
            addAll(activeTrackIds)
            lastMutationRevisionByTrack.forEach { (trackId, mutationRevision) ->
                if (mutationRevision > snapshot.revision) add(trackId)
            }
        }
    }

    private companion object {
        const val DEFAULT_STRIPE_COUNT = 64
    }
}
