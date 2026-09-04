package com.xymusic.app.feature.player.data.media

import android.os.Bundle
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.Timeline
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.source.SinglePeriodTimeline
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.player.adapter.media3.PlaybackMediaMetadata
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@UnstableApi
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class GrantResolvingTimelineTest {
    @Test
    fun eventHlsTimelineIsPublishedAsFiniteSeekableTrack() {
        val publishedItem =
            MediaItem
                .Builder()
                .setMediaId("queue-1")
                .setMediaMetadata(
                    MediaMetadata
                        .Builder()
                        .setDurationMs(TRACK_DURATION_MS)
                        .setExtras(
                            Bundle().apply {
                                putLong(PlaybackMediaMetadata.EXTRA_DURATION_MS, TRACK_DURATION_MS)
                            },
                        )
                        .build(),
                )
                .build()
        val childItem =
            MediaItem
                .Builder()
                .setMediaId("resolved")
                .setLiveConfiguration(
                    MediaItem.LiveConfiguration.Builder()
                        .setTargetOffsetMs(3_000)
                        .build(),
                )
                .build()
        val childTimeline =
            SinglePeriodTimeline(
                C.TIME_UNSET,
                false,
                true,
                true,
                null,
                childItem,
            )

        val publishedTimeline = childTimeline.withMediaItem(publishedItem)
        val window = publishedTimeline.getWindow(0, Timeline.Window(), 0)
        val period = publishedTimeline.getPeriod(0, Timeline.Period(), true)

        assertThat(window.mediaItem).isEqualTo(publishedItem)
        assertThat(window.durationUs).isEqualTo(TRACK_DURATION_MS * 1_000L)
        assertThat(window.isSeekable).isTrue()
        // isDynamic remains true for an active HLS Event playlist so ExoPlayer continues polling and does not end early
        assertThat(window.isDynamic).isTrue()
        // isLive remains false because liveConfiguration is cleared, ensuring system media controls show seekbar and duration
        assertThat(window.isLive()).isFalse()
        assertThat(period.durationUs).isEqualTo(TRACK_DURATION_MS * 1_000L)
    }

    @Test
    fun knownChildDurationIsPreservedWhenMetadataHasNoDuration() {
        val item = MediaItem.Builder().setMediaId("queue-1").build()
        val childTimeline =
            SinglePeriodTimeline(
                25_000_000L,
                false,
                true,
                true,
                null,
                MediaItem
                    .Builder()
                    .setMediaId("resolved")
                    .setLiveConfiguration(
                        MediaItem.LiveConfiguration.Builder()
                            .setTargetOffsetMs(3_000)
                            .build(),
                    )
                    .build(),
            )

        val publishedTimeline = childTimeline.withMediaItem(item)
        val window = publishedTimeline.getWindow(0, Timeline.Window(), 0)

        assertThat(window.durationUs).isEqualTo(25_000_000L)
        assertThat(window.isSeekable).isTrue()
        assertThat(window.isDynamic).isTrue()
        assertThat(window.isLive()).isFalse()
    }

    @Test
    fun staticChildTimelineRemainsNonDynamic() {
        val item = MediaItem.Builder().setMediaId("queue-1").build()
        val childTimeline =
            SinglePeriodTimeline(
                TRACK_DURATION_MS * 1_000L,
                true,
                false,
                false,
                null,
                MediaItem.Builder().setMediaId("resolved").build(),
            )

        val publishedTimeline = childTimeline.withMediaItem(item)
        val window = publishedTimeline.getWindow(0, Timeline.Window(), 0)

        assertThat(window.durationUs).isEqualTo(TRACK_DURATION_MS * 1_000L)
        assertThat(window.isSeekable).isTrue()
        assertThat(window.isDynamic).isFalse()
        assertThat(window.isLive()).isFalse()
    }

    private companion object {
        const val TRACK_DURATION_MS = 180_000L
    }
}
