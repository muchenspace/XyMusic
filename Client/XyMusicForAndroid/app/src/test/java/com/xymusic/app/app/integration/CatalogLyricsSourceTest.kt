package com.xymusic.app.app.integration

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.model.media.Lyrics
import com.xymusic.app.core.model.media.LyricsFormat
import com.xymusic.app.core.model.media.LyricsTiming
import org.junit.Test

class CatalogLyricsSourceTest {
    @Test
    fun playbackSelectionMatchesTheServerDefaultLanguageAndIdOrder() {
        val lyrics = listOf(
            lyric(id = "z", language = "en", isDefault = true),
            lyric(id = "b", language = "zh", isDefault = false),
            lyric(id = "b", language = "en", isDefault = true),
            lyric(id = "a", language = "en", isDefault = true),
        )

        assertThat(selectPlaybackLyrics(lyrics)?.id).isEqualTo("a")
        assertThat(selectPlaybackLyrics(emptyList())).isNull()
    }

    private fun lyric(id: String, language: String, isDefault: Boolean) = Lyrics(
        id = id,
        trackId = "track",
        language = language,
        format = LyricsFormat.LRC,
        timing = LyricsTiming.LINE,
        content = "[00:00]line",
        trackVersion = 1,
        updatedAtEpochMillis = 1,
        isDefault = isDefault,
    )
}
