package com.xymusic.app.feature.player.domain

class OfflineTrackUseCases(private val repository: OfflineTrackRepository) {
    fun observeAll() = repository.observeAll()

    fun observeDownloaded(trackId: String) = repository.observeDownloaded(trackId)

    suspend fun download(trackId: String) = repository.download(trackId)

    suspend fun remove(trackId: String) = repository.remove(trackId)
}
