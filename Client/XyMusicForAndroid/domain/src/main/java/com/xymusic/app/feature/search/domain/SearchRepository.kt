package com.xymusic.app.feature.search.domain

import androidx.paging.PagingData
import com.xymusic.app.core.model.media.Album
import com.xymusic.app.core.model.media.Artist
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.feature.search.domain.model.SearchHistoryItem
import com.xymusic.app.feature.search.domain.model.SearchOverview
import com.xymusic.app.feature.search.domain.model.SearchQuery
import com.xymusic.app.feature.search.domain.model.SearchScope
import kotlinx.coroutines.flow.Flow

interface SearchRepository {
    fun observeOverview(query: SearchQuery): Flow<SearchOverview?>

    suspend fun refreshOverview(query: SearchQuery): SearchResult<Unit>

    fun pagedTracks(query: SearchQuery): Flow<PagingData<Track>>

    fun pagedArtists(query: SearchQuery): Flow<PagingData<Artist>>

    fun pagedAlbums(query: SearchQuery): Flow<PagingData<Album>>

    fun observeHistory(): Flow<List<SearchHistoryItem>>

    suspend fun record(query: SearchQuery, scope: SearchScope): SearchResult<Unit>

    suspend fun delete(query: SearchQuery, scope: SearchScope): SearchResult<Unit>

    suspend fun clearHistory(): SearchResult<Unit>
}

sealed interface SearchResult<out T> {
    data class Success<T>(val value: T) : SearchResult<T>

    data class Failure(val error: DomainError) : SearchResult<Nothing>
}
