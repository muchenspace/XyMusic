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
    override fun observe(trackId: String): Flow<Lyrics?> = catalogUseCases.observeTrack(trackId).map { detail ->
        selectPlaybackLyrics(detail?.lyrics.orEmpty())
    }

    override suspend fun refresh(trackId: String) {
        catalogUseCases.refreshTrack(trackId)
    }
}

internal fun selectPlaybackLyrics(lyrics: List<Lyrics>): Lyrics? = lyrics.minWithOrNull(LYRIC_SELECTION_ORDER)

private val LYRIC_SELECTION_ORDER =
    compareByDescending<Lyrics>(Lyrics::isDefault)
        .thenBy(Lyrics::language)
        .thenBy(Lyrics::id)
