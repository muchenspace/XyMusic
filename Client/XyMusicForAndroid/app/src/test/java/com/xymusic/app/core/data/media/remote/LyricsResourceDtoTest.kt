package com.xymusic.app.core.data.media.remote

import com.google.common.truth.Truth.assertThat
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import org.junit.Test

class LyricsResourceDtoTest {
    @Test
    fun timingIsRequiredInTheServerResponse() {
        val failure =
            runCatching {
                Json.decodeFromString<LyricsResourceDto>(
                    """{"id":"lyrics-1","trackId":"track-1","language":"und","format":"LRC","content":"[00:01.00]line","isDefault":true,"trackVersion":1,"updatedAt":"2026-07-28T00:00:00Z"}""",
                )
            }.exceptionOrNull()

        assertThat(failure).isInstanceOf(SerializationException::class.java)
    }
}
