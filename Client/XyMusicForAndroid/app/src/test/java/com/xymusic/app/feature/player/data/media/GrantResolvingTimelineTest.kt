package com.xymusic.app.feature.player.data.media

import android.os.Bundle
import android.os.Handler
import android.os.HandlerThread
import android.os.Looper
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.Timeline
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.TransferListener
import androidx.media3.exoplayer.analytics.PlayerId
import androidx.media3.exoplayer.source.BaseMediaSource
import androidx.media3.exoplayer.source.MediaPeriod
import androidx.media3.exoplayer.source.MediaSource
import androidx.media3.exoplayer.source.SinglePeriodTimeline
import androidx.media3.exoplayer.upstream.Allocator
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.database.entity.OfflineTrackEntity
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.feature.player.adapter.media3.PlaybackMediaMetadata
import com.xymusic.app.feature.player.adapter.media3.PlaybackMediaUri
import com.xymusic.app.feature.player.domain.PlaybackGrant
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.model.PlayerFailure
import java.io.IOException
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@UnstableApi
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class GrantResolvingTimelineTest {
    @Test
    fun unsetDurationTimelineIsPublishedAsFiniteSeekableTrack() {
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
        // isDynamic remains true for an active stream so ExoPlayer does not end early
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

    @Test
    fun prepareSourceConfinesChildPreparationAndRefreshToPlaybackLooper() {
        val playbackThread = HandlerThread("test-playback-looper").apply { start() }
        try {
            val playbackHandler = Handler(playbackThread.looper)
            val creationLooper = AtomicReference<Looper?>()
            val childPrepareLooper = AtomicReference<Looper?>()
            val refreshLooper = AtomicReference<Looper?>()
            val refreshed = CountDownLatch(1)
            val released = CountDownLatch(1)
            val session = FakeSignedInSession()
            val mediaItem =
                MediaItem
                    .Builder()
                    .setMediaId("queue-1")
                    .setUri(PlaybackMediaUri.forTrack(TRACK_ID))
                    .build()
            val source =
                GrantResolvingMediaSource(
                    mediaItem = mediaItem,
                    trackId = TRACK_ID,
                    grantRepository =
                    FakeGrantRepository(
                        PlayerResult.Success(
                            PlaybackGrant(
                                trackId = TRACK_ID,
                                streamUrl = "https://media.example.test/api/v1/streams/$TRACK_ID?ticket=t1",
                                expiresAtEpochMillis = 600_000L,
                                mimeType = "audio/mpeg",
                                codec = "mp3",
                                container = "mp3",
                                bitrate = 320_000,
                                sampleRate = 44_100,
                                contentLength = 4_096L,
                            ),
                        ),
                    ),
                    grantRegistry = PlaybackGrantRegistry(),
                    offlineMediaStore = EmptyOfflineMediaStore,
                    sessionProvider = session,
                    sessionIdentityProvider = session,
                    sessionMutationCoordinator = SessionMutationCoordinator(),
                    createProgressiveSource = { resolvedItem ->
                        creationLooper.set(Looper.myLooper())
                        LooperRecordingMediaSource(resolvedItem, childPrepareLooper)
                    },
                )
            val caller =
                MediaSource.MediaSourceCaller { _, _ ->
                    refreshLooper.set(Looper.myLooper())
                    refreshed.countDown()
                }

            playbackHandler.post {
                source.prepareSource(caller, null, PlayerId.UNSET)
            }

            assertThat(refreshed.await(5, TimeUnit.SECONDS)).isTrue()
            assertThat(playbackThread.looper).isNotSameInstanceAs(Looper.getMainLooper())
            assertThat(creationLooper.get()).isSameInstanceAs(playbackThread.looper)
            assertThat(childPrepareLooper.get()).isSameInstanceAs(playbackThread.looper)
            assertThat(refreshLooper.get()).isSameInstanceAs(playbackThread.looper)

            playbackHandler.post {
                source.releaseSource(caller)
                released.countDown()
            }
            assertThat(released.await(5, TimeUnit.SECONDS)).isTrue()
        } finally {
            playbackThread.quitSafely()
        }
    }

    @Test
    fun prepareSourceConfinesResolutionFailureToPlaybackLooper() {
        val playbackThread = HandlerThread("test-playback-failure-looper").apply { start() }
        try {
            val playbackHandler = Handler(playbackThread.looper)
            val grantQueried = CountDownLatch(1)
            val released = CountDownLatch(1)
            val session = FakeSignedInSession()
            val mediaItem =
                MediaItem
                    .Builder()
                    .setMediaId("queue-1")
                    .setUri(PlaybackMediaUri.forTrack(TRACK_ID))
                    .build()
            val source =
                GrantResolvingMediaSource(
                    mediaItem = mediaItem,
                    trackId = TRACK_ID,
                    grantRepository =
                    FakeGrantRepository(
                        result = PlayerResult.Failure(PlayerFailure.ConnectionUnavailable),
                        onGet = grantQueried::countDown,
                    ),
                    grantRegistry = PlaybackGrantRegistry(),
                    offlineMediaStore = EmptyOfflineMediaStore,
                    sessionProvider = session,
                    sessionIdentityProvider = session,
                    sessionMutationCoordinator = SessionMutationCoordinator(),
                    createProgressiveSource = { error("Must not create child source on failure") },
                )
            val caller = MediaSource.MediaSourceCaller { _, _ -> }

            playbackHandler.post {
                source.prepareSource(caller, null, PlayerId.UNSET)
            }
            assertThat(grantQueried.await(5, TimeUnit.SECONDS)).isTrue()

            val observedError = AtomicReference<Throwable?>()
            val errorLooper = AtomicReference<Looper?>()
            val checked = CountDownLatch(1)
            val pollFailure =
                object : Runnable {
                    override fun run() {
                        val failure = runCatching { source.maybeThrowSourceInfoRefreshError() }.exceptionOrNull()
                        if (failure != null) {
                            observedError.set(failure)
                            errorLooper.set(Looper.myLooper())
                            source.releaseSource(caller)
                            released.countDown()
                            checked.countDown()
                        } else {
                            playbackHandler.postDelayed(this, 10L)
                        }
                    }
                }
            playbackHandler.post(pollFailure)
            assertThat(checked.await(5, TimeUnit.SECONDS)).isTrue()
            assertThat(released.await(5, TimeUnit.SECONDS)).isTrue()
            assertThat(observedError.get()).isInstanceOf(IOException::class.java)
            assertThat(errorLooper.get()).isSameInstanceAs(playbackThread.looper)
        } finally {
            playbackThread.quitSafely()
        }
    }

    private class FakeSignedInSession :
        AppSessionProvider,
        SessionIdentityProvider {
        private val generation = ServerRuntimeCoordinator().captureGeneration()
        private val identity = ActiveSessionIdentity("user-1", "session-1", generation)
        override val sessionState: StateFlow<AppSessionState> =
            MutableStateFlow(AppSessionState.SignedIn("user-1"))

        override fun activeIdentity(): ActiveSessionIdentity = identity

        override suspend fun restoreSession() = Unit
    }

    private class FakeGrantRepository(
        private val result: PlayerResult<PlaybackGrant>,
        private val onGet: () -> Unit = {},
    ) : PlaybackGrantRepository {
        override suspend fun get(trackId: String, forceRefresh: Boolean): PlayerResult<PlaybackGrant> {
            onGet()
            return result
        }

        override fun invalidate(trackId: String) = Unit

        override fun clear() = Unit
    }

    private object EmptyOfflineMediaStore : OfflineMediaStorePort {
        override fun createDownloadClaim(cacheKey: String): DownloadClaim = error("Not used")

        override suspend fun beginDownload(claim: DownloadClaim) = Unit

        override suspend fun playableTrack(ownerUserId: String, trackId: String): OfflineTrackEntity? = null

        override suspend fun commit(track: OfflineTrackEntity, claim: DownloadClaim): Boolean = false

        override suspend fun remove(ownerUserId: String, trackId: String): Boolean = false

        override suspend fun discardUncommitted(claim: DownloadClaim) = Unit
    }

    private class LooperRecordingMediaSource(
        private val item: MediaItem,
        private val prepareLooper: AtomicReference<Looper?>,
    ) : BaseMediaSource() {
        override fun getMediaItem(): MediaItem = item

        override fun prepareSourceInternal(mediaTransferListener: TransferListener?) {
            prepareLooper.set(Looper.myLooper())
            refreshSourceInfo(
                SinglePeriodTimeline(
                    TRACK_DURATION_MS * 1_000L,
                    true,
                    false,
                    false,
                    null,
                    item,
                ),
            )
        }

        override fun maybeThrowSourceInfoRefreshError() = Unit

        override fun createPeriod(
            id: MediaSource.MediaPeriodId,
            allocator: Allocator,
            startPositionUs: Long,
        ): MediaPeriod = error("Not used")

        override fun releasePeriod(mediaPeriod: MediaPeriod) = Unit

        override fun releaseSourceInternal() = Unit
    }

    private companion object {
        const val TRACK_DURATION_MS = 180_000L
        const val TRACK_ID = "11111111-1111-1111-1111-111111111111"
    }
}
