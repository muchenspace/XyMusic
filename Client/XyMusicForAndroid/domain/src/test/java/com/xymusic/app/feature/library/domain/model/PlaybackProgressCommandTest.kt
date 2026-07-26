package com.xymusic.app.feature.library.domain.model

import com.google.common.truth.Truth.assertThat
import java.util.UUID
import org.junit.Assert.assertThrows
import org.junit.Test

class PlaybackProgressCommandTest {
    private val trackId = UUID.randomUUID().toString()
    private val sessionId = UUID.randomUUID().toString()

    @Test
    fun validCommandIsAccepted() {
        val command =
            PlaybackProgressCommand(
                trackId = trackId,
                playbackSessionId = sessionId,
                positionMs = 0,
                occurredAtEpochMillis = 1_700_000_000_000,
                event = PlaybackEvent.STARTED,
            )

        assertThat(command.positionMs).isEqualTo(0)
    }

    @Test
    fun invalidFieldsAreRejected() {
        assertThrows(IllegalArgumentException::class.java) {
            PlaybackProgressCommand("bad", sessionId, 0, 1, PlaybackEvent.STARTED)
        }
        assertThrows(IllegalArgumentException::class.java) {
            PlaybackProgressCommand(trackId, sessionId, -1, 1, PlaybackEvent.PROGRESS)
        }
        assertThrows(IllegalArgumentException::class.java) {
            PlaybackProgressCommand(trackId, sessionId, 0, 0, PlaybackEvent.PROGRESS)
        }
    }
}
