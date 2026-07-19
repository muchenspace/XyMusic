package com.xymusic.app.feature.player.domain

import com.xymusic.app.core.model.media.Lyrics
import kotlinx.coroutines.flow.Flow

interface LyricsSource {
    fun observe(trackId: String): Flow<List<Lyrics>>

    suspend fun refresh(trackId: String)
}
