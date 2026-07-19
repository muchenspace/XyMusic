package com.xymusic.app.feature.library.domain

import androidx.paging.PagingData
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.feature.library.domain.model.FavoriteSort
import com.xymusic.app.feature.library.domain.model.PlaybackHistoryItem
import com.xymusic.app.feature.library.domain.model.PlaybackProgressCommand
import kotlinx.coroutines.flow.Flow

interface LibraryRepository {
    fun observeIsFavorite(trackId: String): Flow<Boolean>

    fun favoriteTracks(): Flow<PagingData<Track>>

    fun playbackHistory(): Flow<PagingData<PlaybackHistoryItem>>

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
