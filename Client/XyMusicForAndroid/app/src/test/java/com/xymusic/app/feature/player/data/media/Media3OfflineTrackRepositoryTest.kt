package com.xymusic.app.feature.player.data.media

import android.app.Application
import androidx.media3.common.util.UnstableApi
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.seedTrack
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.feature.player.domain.OfflineTrackResult
import com.xymusic.app.feature.player.domain.PlaybackGrant
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.model.PreferredQuality
import dagger.Lazy
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicInteger
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import org.junit.After
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@UnstableApi
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
@OptIn(ExperimentalCoroutinesApi::class)
class Media3OfflineTrackRepositoryTest {
    private lateinit var database: XyMusicDatabase
    private lateinit var cache: FakeOfflineMediaCache
    private lateinit var mediaStore: OfflineMediaStore
    private lateinit var sessionProvider: FakeSessionProvider

    @Before
    fun setUp() {
        database =
            Room
                .inMemoryDatabaseBuilder(
                    ApplicationProvider.getApplicationContext(),
                    XyMusicDatabase::class.java,
                ).allowMainThreadQueries()
                .build()
        cache = FakeOfflineMediaCache()
        mediaStore = OfflineMediaStore(database.offlineTrackDao(), Lazy { cache })
        sessionProvider = FakeSessionProvider("alice", "alice-session-1")
    }

    @After
    fun tearDown() {
        database.close()
    }

    @Test
    fun accountSwitchAwayAndBackWhileGrantIsPendingCannotCommitToReplacementSession() = runTest {
        database.seedTrack(TRACK_ID)
        val grantRepository = BlockingGrantRepository()
        val downloader = RecordingDownloader(cache)
        val repository =
            repository(
                grantRepository = grantRepository,
                downloader = downloader,
                dispatcher = UnconfinedTestDispatcher(testScheduler),
            )

        val result = async { repository.download(TRACK_ID) }
        grantRepository.requestStarted.await()
        sessionProvider.signIn("bob", "bob-session")
        sessionProvider.signIn("alice", "alice-session-2")
        grantRepository.release.complete(Unit)

        assertThat(result.await()).isEqualTo(OfflineTrackResult.Unavailable)
        assertThat(downloader.downloadCount).isEqualTo(0)
        assertThat(database.offlineTrackDao().track("alice", TRACK_ID)).isNull()
        assertThat(database.offlineTrackDao().track("bob", TRACK_ID)).isNull()
    }

    @Test
    fun switchingOwnerDuringMediaTransferDiscardsBytesAndDoesNotPersistMetadata() = runTest {
        database.seedTrack(TRACK_ID)
        val downloader = BlockingDownloader(cache)
        val repository =
            repository(
                grantRepository = ImmediateGrantRepository(),
                downloader = downloader,
                dispatcher = UnconfinedTestDispatcher(testScheduler),
            )

        val result = async { repository.download(TRACK_ID) }
        downloader.downloadStarted.await()
        sessionProvider.signIn("bob", "bob-session")
        downloader.release.complete(Unit)

        assertThat(result.await()).isEqualTo(OfflineTrackResult.Unavailable)
        assertThat(database.offlineTrackDao().track("alice", TRACK_ID)).isNull()
        assertThat(database.offlineTrackDao().track("bob", TRACK_ID)).isNull()
        assertThat(cache.cachedKeys).doesNotContain(CACHE_KEY)
        assertThat(cache.removedKeys).containsExactly(CACHE_KEY)
    }

    @Test
    fun cancellationDuringTransferCannotCommitCompletedBytes() = runTest {
        database.seedTrack(TRACK_ID)
        val downloader = NonCancellableBlockingDownloader(cache)
        val repository =
            repository(
                grantRepository = ImmediateGrantRepository(),
                downloader = downloader,
                dispatcher = UnconfinedTestDispatcher(testScheduler),
            )

        val result = async { repository.download(TRACK_ID) }
        downloader.downloadStarted.await()
        result.cancel()
        downloader.release.complete(Unit)
        val failure = runCatching { result.await() }.exceptionOrNull()

        assertThat(failure).isInstanceOf(CancellationException::class.java)
        assertThat(database.offlineTrackDao().track("alice", TRACK_ID)).isNull()
        assertThat(cache.cachedKeys).doesNotContain(CACHE_KEY)
        assertThat(cache.removedKeys).containsExactly(CACHE_KEY)
    }

    @Test
    fun twoTracksSharingCacheKeyCanDownloadConcurrentlyWithoutDeletingMedia() = runTest {
        database.seedTrack("a")
        database.seedTrack("b")
        val downloader = BarrierDownloader(cache, expectedDownloads = 2)
        val repository =
            repository(
                grantRepository = ImmediateGrantRepository(),
                downloader = downloader,
                dispatcher = UnconfinedTestDispatcher(testScheduler),
            )

        val results =
            listOf("a", "b")
                .map { trackId -> async { repository.download(trackId) } }
                .awaitAll()

        assertThat(results).containsExactly(
            OfflineTrackResult.Success,
            OfflineTrackResult.Success,
        )
        assertThat(database.offlineTrackDao().track("alice", "a")).isNotNull()
        assertThat(database.offlineTrackDao().track("alice", "b")).isNotNull()
        assertThat(cache.cachedKeys).contains(CACHE_KEY)
        assertThat(cache.removedKeys).isEmpty()
    }

    private fun repository(
        grantRepository: PlaybackGrantRepository,
        downloader: OfflineMediaDownloader,
        dispatcher: kotlinx.coroutines.CoroutineDispatcher,
    ) = Media3OfflineTrackRepository(
        offlineTrackDao = database.offlineTrackDao(),
        catalogDao = database.catalogDao(),
        offlineMediaStore = mediaStore,
        offlineMediaDownloader = downloader,
        playbackGrantRepository = grantRepository,
        json = Json,
        clock = Clock.fixed(Instant.ofEpochMilli(10_000), ZoneOffset.UTC),
        sessionProvider = sessionProvider,
        sessionIdentityProvider = sessionProvider,
        sessionMutationCoordinator = SessionMutationCoordinator(),
        ioDispatcher = dispatcher,
    )

    private class FakeSessionProvider(userId: String, sessionId: String) :
        AppSessionProvider,
        SessionIdentityProvider {
        private val generation = ServerRuntimeCoordinator().captureGeneration()
        private val mutableState =
            MutableStateFlow<AppSessionState>(
                AppSessionState.SignedIn(userId),
            )
        private var identity = ActiveSessionIdentity(userId, sessionId, generation)

        override val sessionState: StateFlow<AppSessionState> = mutableState

        override fun activeIdentity(): ActiveSessionIdentity = identity

        fun signIn(userId: String, sessionId: String) {
            identity = ActiveSessionIdentity(userId, sessionId, generation)
            mutableState.value = AppSessionState.SignedIn(userId)
        }

        override suspend fun restoreSession() = Unit
    }

    private class BlockingGrantRepository : PlaybackGrantRepository {
        val requestStarted = CompletableDeferred<Unit>()
        val release = CompletableDeferred<Unit>()

        override suspend fun get(
            trackId: String,
            preferredQuality: PreferredQuality,
            acceptedCodecs: List<String>,
            forceRefresh: Boolean,
        ): PlayerResult<PlaybackGrant> {
            requestStarted.complete(Unit)
            release.await()
            return PlayerResult.Success(grant(trackId))
        }

        override fun invalidate(trackId: String) = Unit

        override fun clear() = Unit
    }

    private class ImmediateGrantRepository : PlaybackGrantRepository {
        override suspend fun get(
            trackId: String,
            preferredQuality: PreferredQuality,
            acceptedCodecs: List<String>,
            forceRefresh: Boolean,
        ): PlayerResult<PlaybackGrant> = PlayerResult.Success(grant(trackId))

        override fun invalidate(trackId: String) = Unit

        override fun clear() = Unit
    }

    private class RecordingDownloader(private val cache: FakeOfflineMediaCache) : OfflineMediaDownloader {
        var downloadCount = 0

        override suspend fun download(grant: PlaybackGrant) {
            downloadCount += 1
            cache.cachedKeys += grant.cacheKey
        }
    }

    private class BlockingDownloader(private val cache: FakeOfflineMediaCache) : OfflineMediaDownloader {
        val downloadStarted = CompletableDeferred<Unit>()
        val release = CompletableDeferred<Unit>()

        override suspend fun download(grant: PlaybackGrant) {
            downloadStarted.complete(Unit)
            release.await()
            cache.cachedKeys += grant.cacheKey
        }
    }

    private class NonCancellableBlockingDownloader(private val cache: FakeOfflineMediaCache) : OfflineMediaDownloader {
        val downloadStarted = CompletableDeferred<Unit>()
        val release = CompletableDeferred<Unit>()

        override suspend fun download(grant: PlaybackGrant) = withContext(NonCancellable) {
            downloadStarted.complete(Unit)
            release.await()
            cache.cachedKeys += grant.cacheKey
        }
    }

    private class BarrierDownloader(private val cache: FakeOfflineMediaCache, private val expectedDownloads: Int) :
        OfflineMediaDownloader {
        private val allDownloadsStarted = CompletableDeferred<Unit>()
        private val startedDownloads = AtomicInteger()

        override suspend fun download(grant: PlaybackGrant) {
            if (startedDownloads.incrementAndGet() == expectedDownloads) {
                allDownloadsStarted.complete(Unit)
            }
            allDownloadsStarted.await()
            cache.cachedKeys += grant.cacheKey
        }
    }

    private class FakeOfflineMediaCache : OfflineMediaCache {
        val cachedKeys: MutableSet<String> = ConcurrentHashMap.newKeySet()
        val pinnedKeys: MutableSet<String> = ConcurrentHashMap.newKeySet()
        val persistentPinnedKeys: MutableSet<String> = ConcurrentHashMap.newKeySet()
        val removedKeys = mutableListOf<String>()

        override fun pin(cacheKey: String) {
            pinnedKeys += cacheKey
        }

        override fun unpin(cacheKey: String) {
            pinnedKeys -= cacheKey
        }

        override fun promotePin(cacheKey: String) {
            persistentPinnedKeys += cacheKey
        }

        override fun isFullyCached(cacheKey: String, contentLength: Long): Boolean =
            contentLength > 0 && cacheKey in cachedKeys

        override suspend fun remove(cacheKey: String) {
            removedKeys += cacheKey
            cachedKeys -= cacheKey
            pinnedKeys -= cacheKey
            persistentPinnedKeys -= cacheKey
        }
    }

    private companion object {
        const val TRACK_ID = "track"
        const val CACHE_KEY = "shared-cache"

        fun grant(trackId: String) = PlaybackGrant(
            trackId = trackId,
            variantId = "variant-$trackId",
            selectedQuality = PreferredQuality.AUTO,
            signedUrl = "https://media.example/$trackId",
            expiresAtEpochMillis = Long.MAX_VALUE,
            mimeType = "audio/mpeg",
            codec = "mp3",
            container = "mp3",
            bitrate = 128_000,
            sampleRate = 44_100,
            contentLength = 128,
            checksumSha256 = null,
            cacheKey = CACHE_KEY,
        )
    }
}
