package com.xymusic.app.core.database

import com.google.common.truth.Truth.assertThat
import org.junit.Test

class RoomConvertersTest {
    @Test
    fun invalidStoredLyricsTimingIsRejected() {
        val failure = runCatching { RoomConverters().stringToLyricsTiming("UNKNOWN") }.exceptionOrNull()

        assertThat(failure).isInstanceOf(IllegalArgumentException::class.java)
    }
}
