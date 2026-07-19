package com.xymusic.app.feature.library.domain

import com.xymusic.app.feature.library.domain.model.FavoriteSort
import com.xymusic.app.feature.library.domain.model.PlaybackProgressCommand
import javax.inject.Inject

class LibraryUseCases
@Inject
constructor(private val repository: LibraryRepository) {
    fun observeIsFavorite(trackId: String) = repository.observeIsFavorite(trackId)

    fun favorites() = repository.favoriteTracks()

    fun history() = repository.playbackHistory()

    suspend fun refreshFavorites(sort: FavoriteSort = FavoriteSort.FAVORITED_DESC) = repository.refreshFavorites(sort)

    suspend fun refreshHistory() = repository.refreshHistory()

    suspend fun setFavorite(trackId: String, favorite: Boolean) = repository.setFavorite(trackId, favorite)

    suspend fun recordPlayback(command: PlaybackProgressCommand) = repository.recordPlayback(command)
}
