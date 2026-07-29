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
    override fun observe(trackId: String): Flow<Lyrics?> =
        catalogUseCases.observeTrack(trackId).map { detail ->
            val lyrics = detail?.lyrics.orEmpty()
            require(lyrics.size <= 1) { "Cached track contains multiple lyric resources" }
            lyrics.singleOrNull()
        }

    override suspend fun refresh(trackId: String) {
        catalogUseCases.refreshTrack(trackId)
    }
}
