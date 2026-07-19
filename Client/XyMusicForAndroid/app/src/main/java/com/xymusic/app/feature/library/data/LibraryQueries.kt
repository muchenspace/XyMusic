package com.xymusic.app.feature.library.data

import androidx.paging.Pager
import androidx.paging.PagingConfig
import androidx.paging.PagingData
import androidx.paging.map
import com.xymusic.app.core.data.media.toDomain
import com.xymusic.app.core.database.dao.LibraryDao
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.feature.library.domain.model.PlaybackHistoryItem
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map

@OptIn(ExperimentalCoroutinesApi::class)
internal class LibraryQueries(private val libraryDao: LibraryDao, private val sessionProvider: AppSessionProvider) {
    fun observeIsFavorite(trackId: String): Flow<Boolean> = sessionProvider.sessionState.flatMapLatest { state ->
        val owner = (state as? AppSessionState.SignedIn)?.userId
        if (owner == null) {
            flowOf(false)
        } else {
            libraryDao.observeFavorite(owner, trackId)
        }
    }

    fun favoriteTracks(): Flow<PagingData<Track>> = sessionProvider.sessionState.flatMapLatest { state ->
        val owner = (state as? AppSessionState.SignedIn)?.userId
        if (owner == null) {
            flowOf(PagingData.empty<Track>())
        } else {
            Pager(
                config = PagingConfig(pageSize = 30, enablePlaceholders = false),
                pagingSourceFactory = { libraryDao.pagedFavoriteTracks(owner) },
            ).flow.map { data -> data.map { it.toDomain() } }
        }
    }

    fun playbackHistory(): Flow<PagingData<PlaybackHistoryItem>> = sessionProvider.sessionState.flatMapLatest { state ->
        val owner = (state as? AppSessionState.SignedIn)?.userId
        if (owner == null) {
            flowOf(PagingData.empty<PlaybackHistoryItem>())
        } else {
            Pager(
                config = PagingConfig(pageSize = 30, enablePlaceholders = false),
                pagingSourceFactory = { libraryDao.pagedHistory(owner) },
            ).flow.map { data -> data.map { it.toDomain() } }
        }
    }
}
