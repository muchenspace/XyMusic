package com.xymusic.app.feature.player.service

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.player.data.media.PlaybackMediaMetadata
import com.xymusic.app.feature.player.domain.StoredPlaybackQueueItem
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

@RunWith(RobolectricTestRunner::class)
class PlaybackServiceQueueRestoreTest {
    @Test
    fun currentIndexIsRecomputedAfterInvalidItemsAreFiltered() {
        val invalid =
            queueItem(
                queueItemId = "invalid",
                position = 0,
                trackId = "not-a-uuid",
                isCurrent = false,
                resumePositionMs = 0,
            )
        val current =
            queueItem(
                queueItemId = "current",
                position = 1,
                trackId = "00000000-0000-0000-0000-000000000001",
                isCurrent = true,
                resumePositionMs = 12_345,
            )
        val next =
            queueItem(
                queueItemId = "next",
                position = 2,
                trackId = "00000000-0000-0000-0000-000000000002",
                isCurrent = false,
                resumePositionMs = 0,
            )

        val restored = selectRestorablePlaybackQueue(listOf(next, invalid, current))

        assertThat(restored).isNotNull()
        assertThat(restored!!.items.map(StoredPlaybackQueueItem::queueItemId))
            .containsExactly("current", "next")
            .inOrder()
        assertThat(restored.currentIndex).isEqualTo(0)
        assertThat(restored.startPositionMs).isEqualTo(12_345)
    }

    @Test
    fun missingCurrentFallsBackToFirstValidItemAndClampsPosition() {
        val restored =
            selectRestorablePlaybackQueue(
                listOf(
                    queueItem(
                        queueItemId = "first",
                        position = 0,
                        trackId = "00000000-0000-0000-0000-000000000001",
                        isCurrent = false,
                        resumePositionMs = -1,
                    ),
                ),
            )

        assertThat(restored).isNotNull()
        assertThat(restored!!.currentIndex).isEqualTo(0)
        assertThat(restored.startPositionMs).isEqualTo(0)
    }

    @Test
    fun completedOrOvershotCurrentItemRestartsFromBeginning() {
        listOf(4_000L, 4_999L, 5_000L, 5_001L).forEach { resumePositionMs ->
            val restored =
                selectRestorablePlaybackQueue(
                    listOf(
                        queueItem(
                            queueItemId = "completed",
                            position = 0,
                            trackId = "00000000-0000-0000-0000-000000000001",
                            isCurrent = true,
                            resumePositionMs = resumePositionMs,
                            durationMs = 5_000,
                        ),
                    ),
                )

            assertThat(restored).isNotNull()
            assertThat(restored!!.startPositionMs).isEqualTo(0)
        }
    }

    @Test
    fun currentItemBeforeCompletionToleranceKeepsItsResumePosition() {
        val restored =
            selectRestorablePlaybackQueue(
                listOf(
                    queueItem(
                        queueItemId = "in-progress",
                        position = 0,
                        trackId = "00000000-0000-0000-0000-000000000001",
                        isCurrent = true,
                        resumePositionMs = 3_999,
                        durationMs = 5_000,
                    ),
                ),
            )

        assertThat(restored).isNotNull()
        assertThat(restored!!.startPositionMs).isEqualTo(3_999)
    }

    @Test
    fun restoredMediaItemPreservesSignedArtworkUrlAndStableCacheKey() {
        val stored =
            queueItem(
                queueItemId = "current",
                position = 0,
                trackId = "00000000-0000-0000-0000-000000000001",
                isCurrent = true,
                resumePositionMs = 0,
                artworkUrl = "https://media.example/artwork.jpg?signature=expired",
                artworkCacheKey = "artwork:asset-1:generation-2",
            )

        val mediaItem = stored.toPlaybackMediaItem()

        assertThat(mediaItem.mediaMetadata.artworkUri.toString()).isEqualTo(stored.artworkUrl)
        assertThat(
            mediaItem.mediaMetadata.extras?.getString(
                PlaybackMediaMetadata.EXTRA_ARTWORK_CACHE_KEY,
            ),
        ).isEqualTo(stored.artworkCacheKey)
    }

    private fun queueItem(
        queueItemId: String,
        position: Int,
        trackId: String,
        isCurrent: Boolean,
        resumePositionMs: Long,
        artworkUrl: String? = null,
        artworkCacheKey: String? = null,
        durationMs: Long = 0,
    ) = StoredPlaybackQueueItem(
        queueItemId = queueItemId,
        position = position,
        trackId = trackId,
        variantId = null,
        stableCacheKey = null,
        resumePositionMs = resumePositionMs,
        isCurrent = isCurrent,
        enqueuedAtEpochMillis = 1,
        title = trackId,
        artistNames = emptyList(),
        albumTitle = null,
        artworkUrl = artworkUrl,
        artworkCacheKey = artworkCacheKey,
        durationMs = durationMs,
    )
}
