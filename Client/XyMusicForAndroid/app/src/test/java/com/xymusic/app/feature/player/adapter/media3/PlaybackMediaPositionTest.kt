package com.xymusic.app.feature.player.adapter.media3

import android.os.Bundle
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import com.google.common.truth.Truth.assertThat
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class PlaybackMediaPositionTest {
    @Test
    fun globalDurationReadsTheStandardMediaMetadataDuration() {
        val item = mediaItem(durationMs = 180_000)

        assertThat(item.playbackMetadataDurationMs()).isEqualTo(180_000L)
        assertThat(item.globalPlaybackDurationMs(0)).isEqualTo(180_000L)
        assertThat(item.globalPlaybackPositionMs(12_000)).isEqualTo(12_000L)
    }

    @Test
    fun legacyExtraRemainsACompatibleDurationFallback() {
        val item = MediaItem
            .Builder()
            .setMediaMetadata(
                MediaMetadata
                    .Builder()
                    .setExtras(
                        Bundle().apply {
                            putLong(PlaybackMediaMetadata.EXTRA_DURATION_MS, 180_000)
                        },
                    )
                    .build(),
            )
            .build()

        assertThat(item.playbackMetadataDurationMs()).isEqualTo(180_000L)
        assertThat(item.globalPlaybackDurationMs(0)).isEqualTo(180_000L)
    }

    private fun mediaItem(durationMs: Long): MediaItem {
        return MediaItem
            .Builder()
            .setMediaId("queue-1")
            .setMediaMetadata(
                MediaMetadata
                    .Builder()
                    .setDurationMs(durationMs)
                    .build(),
            )
            .build()
    }
}
