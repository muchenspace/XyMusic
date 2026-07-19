package com.xymusic.app.feature.search.domain

import com.xymusic.app.feature.search.domain.model.SearchQuery
import com.xymusic.app.feature.search.domain.model.SearchScope
import javax.inject.Inject

class SearchUseCases
@Inject
constructor(private val repository: SearchRepository) {
    fun observeOverview(query: SearchQuery) = repository.observeOverview(query)

    suspend fun refreshOverview(query: SearchQuery) = repository.refreshOverview(query)

    fun tracks(query: SearchQuery) = repository.pagedTracks(query)

    fun artists(query: SearchQuery) = repository.pagedArtists(query)

    fun albums(query: SearchQuery) = repository.pagedAlbums(query)

    fun observeHistory() = repository.observeHistory()

    suspend fun record(query: SearchQuery, scope: SearchScope) = repository.record(query, scope)

    suspend fun delete(query: SearchQuery, scope: SearchScope) = repository.delete(query, scope)

    suspend fun clearHistory() = repository.clearHistory()
}
