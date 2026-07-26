package com.xymusic.app.feature.library.domain

import com.xymusic.app.core.model.media.Track
import com.xymusic.app.domain.error.DomainError
import com.xymusic.app.domain.paging.PagedStream
import com.xymusic.app.feature.library.domain.model.FavoriteSort
import com.xymusic.app.feature.library.domain.model.PlaybackHistoryItem
import com.xymusic.app.feature.library.domain.model.PlaybackProgressCommand
import kotlinx.coroutines.flow.Flow

interface LibraryRepository {
    fun observeIsFavorite(trackId: String): Flow<Boolean>

    fun favoriteTracks(): PagedStream<Track>

    fun playbackHistory(): PagedStream<PlaybackHistoryItem>

    suspend fun refreshFavorites(sort: FavoriteSort = FavoriteSort.FAVORITED_DESC): LibraryResult<Unit>

    suspend fun refreshHistory(): LibraryResult<Unit>

    suspend fun setFavorite(trackId: String, favorite: Boolean): LibraryResult<Unit>

    suspend fun recordPlayback(command: PlaybackProgressCommand): LibraryResult<Unit>

    suspend fun recordPlaybackForOwner(ownerUserId: String, command: PlaybackProgressCommand): LibraryResult<Unit> =
        recordPlayback(command)
}

sealed interface LibraryResult<out T> {
    data class Success<T>(val value: T) : LibraryResult<T>

    data class Failure(val error: DomainError) : LibraryResult<Nothing>
}
