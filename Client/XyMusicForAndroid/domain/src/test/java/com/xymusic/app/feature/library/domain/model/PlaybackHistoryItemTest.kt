package com.xymusic.app.feature.library.domain.model

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.model.media.Track
import org.junit.Test

class PlaybackHistoryItemTest {
    @Test
    fun completedOrNearEndHistoryRestartsFromZeroWhileMidTrackResumes() {
        val completed = historyItem(lastPositionMs = 180_000, completed = true)
        val completedEarly = historyItem(lastPositionMs = 42_000, completed = true)
        val nearEnd = historyItem(lastPositionMs = 179_200, completed = false)
        val inProgress = historyItem(lastPositionMs = 42_000, completed = false)

        assertThat(completed.resumePositionMs()).isEqualTo(0L)
        assertThat(completedEarly.resumePositionMs()).isEqualTo(0L)
        assertThat(nearEnd.resumePositionMs()).isEqualTo(0L)
        assertThat(inProgress.resumePositionMs()).isEqualTo(42_000L)
    }

    private fun historyItem(lastPositionMs: Long, completed: Boolean) = PlaybackHistoryItem(
        track = Track(
            id = "00000000-0000-0000-0000-000000000001",
            title = "Track",
            artists = emptyList(),
            album = null,
            artwork = null,
            durationMs = 180_000,
            trackNumber = 1,
            discNumber = 1,
            publishedAtEpochMillis = 1_000,
        ),
        lastPositionMs = lastPositionMs,
        playCount = 1,
        lastPlayedAtEpochMillis = 2_000,
        completed = completed,
        updatedAtEpochMillis = 2_000,
    )
}
