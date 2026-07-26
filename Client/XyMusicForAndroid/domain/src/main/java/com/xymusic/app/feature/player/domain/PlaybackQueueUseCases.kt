package com.xymusic.app.feature.player.domain

class PlaybackQueueUseCases(private val store: PlaybackQueueStore) {
    fun observe() = store.observe()

    suspend fun replace(ownerUserId: String, items: List<StoredPlaybackQueueItem>) = store.replace(ownerUserId, items)

    suspend fun updatePosition(ownerUserId: String, queueItemId: String, positionMs: Long) =
        store.updatePosition(ownerUserId, queueItemId, positionMs)

    suspend fun setCurrent(ownerUserId: String, queueItemId: String, positionMs: Long) =
        store.setCurrent(ownerUserId, queueItemId, positionMs)

    suspend fun clear(ownerUserId: String) = store.clear(ownerUserId)
}
