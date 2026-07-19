package com.xymusic.app.feature.playlist.data

import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

internal class PlaylistMutationCoordinator(stripeCount: Int = DEFAULT_STRIPE_COUNT) {
    private val mutexes: Array<Mutex>

    init {
        require(stripeCount > 0) { "Stripe count must be positive" }
        mutexes = Array(stripeCount) { Mutex() }
    }

    suspend fun <T> serialize(playlistId: String, operation: suspend () -> T): T {
        val index = (playlistId.hashCode() and Int.MAX_VALUE) % mutexes.size
        return mutexes[index].withLock { operation() }
    }

    private companion object {
        const val DEFAULT_STRIPE_COUNT = 64
    }
}
