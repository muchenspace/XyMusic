package com.xymusic.app.app.integration

import com.xymusic.app.core.model.media.Lyrics
import com.xymusic.app.feature.catalog.domain.CatalogUseCases
import com.xymusic.app.feature.player.domain.LyricsSource
import javax.inject.Inject
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

class CatalogLyricsSource
@Inject
constructor(private val catalogUseCases: CatalogUseCases) : LyricsSource {
    override fun observe(trackId: String): Flow<List<Lyrics>> =
        catalogUseCases.observeTrack(trackId).map { detail -> detail?.lyrics.orEmpty() }

    override suspend fun refresh(trackId: String) {
        catalogUseCases.refreshTrack(trackId)
    }
}
