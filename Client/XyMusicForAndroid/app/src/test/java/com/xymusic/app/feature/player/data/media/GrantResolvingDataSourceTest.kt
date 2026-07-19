package com.xymusic.app.feature.player.data.media

import android.app.Application
import android.net.Uri
import androidx.media3.common.C
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.DataSpec
import androidx.media3.datasource.TransferListener
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.entity.OfflineTrackEntity
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.feature.player.domain.PlaybackGrant
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.model.PlayerFailure
import com.xymusic.app.feature.player.domain.model.PreferredQuality
import dagger.Lazy
import java.io.IOException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@UnstableApi
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class GrantResolvingDataSourceTest {
    private lateinit var database: XyMusicDatabase
    private lateinit var cache: FakeOfflineMediaCache

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
    }

    @After
    fun tearDown() {
        database.close()
    }

    @Test
    fun activeOwnerCanOpenItsOfflineTrackWithoutRequestingAGrant() {
        val offlineTrack = offlineTrack("alice")
        runBlocking { database.offlineTrackDao().upsert(offlineTrack) }
        cache.cachedKeys += offlineTrack.cacheKey
        val grantRepository = UnavailableGrantRepository()
        val onlineFactory = RecordingDataSourceFactory()
        val offlineFactory = RecordingDataSourceFactory()
        val source =
            source(
                ownerState = AppSessionState.SignedIn("alice"),
                grantRepository = grantRepository,
                onlineFactory = onlineFactory,
                offlineFactory = offlineFactory,
            )

        source.open(
            DataSpec.Builder().setUri(PlaybackMediaUri.forTrack(offlineTrack.trackId)).build(),
        )

        assertThat(grantRepository.requestCount).isEqualTo(0)
        assertThat(offlineFactory.createCount).isEqualTo(1)
        assertThat(offlineFactory.lastOpenedSpec?.key).isEqualTo(offlineTrack.cacheKey)
        assertThat(offlineFactory.lastOpenedSpec?.uri?.scheme).isEqualTo("xymusic")
        assertThat(offlineFactory.lastOpenedSpec?.length).isEqualTo(offlineTrack.contentLength)
        assertThat(onlineFactory.createCount).isEqualTo(0)
    }

    @Test
    fun offlineTailIsBoundedToRemainingContentLength() {
        val offlineTrack = offlineTrack("alice")
        runBlocking { database.offlineTrackDao().upsert(offlineTrack) }
        cache.cachedKeys += offlineTrack.cacheKey
        val grantRepository = UnavailableGrantRepository()
        val onlineFactory = RecordingDataSourceFactory()
        val offlineFactory = RecordingDataSourceFactory()
        val source =
            source(
                ownerState = AppSessionState.SignedIn("alice"),
                grantRepository = grantRepository,
                onlineFactory = onlineFactory,
                offlineFactory = offlineFactory,
            )

        source.open(
            DataSpec
                .Builder()
                .setUri(PlaybackMediaUri.forTrack(offlineTrack.trackId))
                .setPosition(96)
                .build(),
        )

        assertThat(offlineFactory.lastOpenedSpec?.position).isEqualTo(96)
        assertThat(offlineFactory.lastOpenedSpec?.length).isEqualTo(32)
        assertThat(grantRepository.requestCount).isEqualTo(0)
        assertThat(onlineFactory.createCount).isEqualTo(0)
    }

    @Test
    fun offlineTailPreservesAShorterRequestedLength() {
        val offlineTrack = offlineTrack("alice")
        runBlocking { database.offlineTrackDao().upsert(offlineTrack) }
        cache.cachedKeys += offlineTrack.cacheKey
        val offlineFactory = RecordingDataSourceFactory()
        val source =
            source(
                ownerState = AppSessionState.SignedIn("alice"),
                grantRepository = UnavailableGrantRepository(),
                onlineFactory = RecordingDataSourceFactory(),
                offlineFactory = offlineFactory,
            )

        source.open(
            DataSpec
                .Builder()
                .setUri(PlaybackMediaUri.forTrack(offlineTrack.trackId))
                .setPosition(96)
                .setLength(8)
                .build(),
        )

        assertThat(offlineFactory.lastOpenedSpec?.length).isEqualTo(8)
    }

    @Test
    fun offlinePositionAtOrBeyondContentReturnsEofWithoutGrantOrFactories() {
        val offlineTrack = offlineTrack("alice")
        runBlocking { database.offlineTrackDao().upsert(offlineTrack) }
        cache.cachedKeys += offlineTrack.cacheKey
        val grantRepository = UnavailableGrantRepository()
        val onlineFactory = RecordingDataSourceFactory()
        val offlineFactory = RecordingDataSourceFactory()

        listOf(offlineTrack.contentLength, offlineTrack.contentLength + 1).forEach { position ->
            val source =
                source(
                    ownerState = AppSessionState.SignedIn("alice"),
                    grantRepository = grantRepository,
                    onlineFactory = onlineFactory,
                    offlineFactory = offlineFactory,
                )

            val openedLength =
                source.open(
                    DataSpec
                        .Builder()
                        .setUri(PlaybackMediaUri.forTrack(offlineTrack.trackId))
                        .setPosition(position)
                        .build(),
                )

            assertThat(openedLength).isEqualTo(0)
            assertThat(source.read(ByteArray(0), 0, 0)).isEqualTo(0)
            assertThat(source.read(ByteArray(1), 0, 1)).isEqualTo(C.RESULT_END_OF_INPUT)
            source.close()
        }

        assertThat(grantRepository.requestCount).isEqualTo(0)
        assertThat(offlineFactory.createCount).isEqualTo(0)
        assertThat(onlineFactory.createCount).isEqualTo(0)
    }

    @Test
    fun offlineCacheGapDoesNotFallBackToGrantOrNetwork() {
        val offlineTrack = offlineTrack("alice")
        runBlocking { database.offlineTrackDao().upsert(offlineTrack) }
        cache.cachedKeys += offlineTrack.cacheKey
        val grantRepository = UnavailableGrantRepository()
        val onlineFactory = RecordingDataSourceFactory()
        val offlineFactory = RecordingDataSourceFactory(openFailure = IOException("cache gap"))
        val source =
            source(
                ownerState = AppSessionState.SignedIn("alice"),
                grantRepository = grantRepository,
                onlineFactory = onlineFactory,
                offlineFactory = offlineFactory,
            )

        val failure =
            runCatching {
                source.open(
                    DataSpec.Builder().setUri(PlaybackMediaUri.forTrack(offlineTrack.trackId)).build(),
                )
            }.exceptionOrNull()

        assertThat(failure).isInstanceOf(IOException::class.java)
        assertThat(failure?.message).isEqualTo("cache gap")
        assertThat(offlineFactory.createCount).isEqualTo(1)
        assertThat(grantRepository.requestCount).isEqualTo(0)
        assertThat(onlineFactory.createCount).isEqualTo(0)
    }

    @Test
    fun onlinePlaybackResolvesHttpsGrantBeforeOpeningNetworkBackedFactory() {
        val grantRepository = AvailableGrantRepository()
        val onlineFactory = RecordingDataSourceFactory()
        val offlineFactory = RecordingDataSourceFactory()
        val source =
            source(
                ownerState = AppSessionState.SignedIn("alice"),
                grantRepository = grantRepository,
                onlineFactory = onlineFactory,
                offlineFactory = offlineFactory,
            )

        source.open(
            DataSpec.Builder().setUri(PlaybackMediaUri.forTrack(TRACK_ID)).build(),
        )

        assertThat(grantRepository.requestCount).isEqualTo(1)
        assertThat(offlineFactory.createCount).isEqualTo(0)
        assertThat(onlineFactory.createCount).isEqualTo(1)
        assertThat(onlineFactory.lastOpenedSpec?.uri?.scheme).isEqualTo("https")
        assertThat(onlineFactory.lastOpenedSpec?.uri?.toString()).isEqualTo(SIGNED_URL)
        assertThat(onlineFactory.lastOpenedSpec?.key).isEqualTo(NETWORK_CACHE_KEY)
    }

    @Test
    fun compatibleCodecFallbackBypassesDownloadedFlacAndOpensNetworkGrant() {
        val offlineTrack = offlineTrack("alice")
        runBlocking { database.offlineTrackDao().upsert(offlineTrack) }
        cache.cachedKeys += offlineTrack.cacheKey
        val grantRepository = AvailableGrantRepository(compatibleCodecFallbackEnabled = true)
        val onlineFactory = RecordingDataSourceFactory()
        val offlineFactory = RecordingDataSourceFactory()
        val source =
            source(
                ownerState = AppSessionState.SignedIn("alice"),
                grantRepository = grantRepository,
                onlineFactory = onlineFactory,
                offlineFactory = offlineFactory,
            )

        source.open(
            DataSpec.Builder().setUri(PlaybackMediaUri.forTrack(offlineTrack.trackId)).build(),
        )

        assertThat(grantRepository.requestCount).isEqualTo(1)
        assertThat(offlineFactory.createCount).isEqualTo(0)
        assertThat(onlineFactory.createCount).isEqualTo(1)
        assertThat(onlineFactory.lastOpenedSpec?.uri?.toString()).isEqualTo(SIGNED_URL)
    }

    @Test
    fun anotherOwnerCannotOpenThePreviousOwnersOfflineTrack() {
        val offlineTrack = offlineTrack("alice")
        runBlocking { database.offlineTrackDao().upsert(offlineTrack) }
        cache.cachedKeys += offlineTrack.cacheKey
        val grantRepository = UnavailableGrantRepository()
        val onlineFactory = RecordingDataSourceFactory()
        val offlineFactory = RecordingDataSourceFactory()
        val source =
            source(
                ownerState = AppSessionState.SignedIn("bob"),
                grantRepository = grantRepository,
                onlineFactory = onlineFactory,
                offlineFactory = offlineFactory,
            )

        val failure =
            runCatching {
                source.open(
                    DataSpec.Builder().setUri(PlaybackMediaUri.forTrack(offlineTrack.trackId)).build(),
                )
            }.exceptionOrNull()

        assertThat(failure).isInstanceOf(IOException::class.java)
        assertThat(grantRepository.requestCount).isEqualTo(1)
        assertThat(onlineFactory.lastOpenedSpec).isNull()
        assertThat(offlineFactory.lastOpenedSpec).isNull()
        assertThat(
            runBlocking { database.offlineTrackDao().track("alice", offlineTrack.trackId) },
        ).isEqualTo(offlineTrack)
        assertThat(cache.cachedKeys).contains(offlineTrack.cacheKey)
    }

    @Test
    fun switchingOwnerAfterOpeningOfflineMediaStopsFurtherReads() {
        val offlineTrack = offlineTrack("alice")
        runBlocking { database.offlineTrackDao().upsert(offlineTrack) }
        cache.cachedKeys += offlineTrack.cacheKey
        val grantRepository = UnavailableGrantRepository()
        val onlineFactory = RecordingDataSourceFactory()
        val offlineFactory = RecordingDataSourceFactory()
        val sessionProvider = FakeSessionProvider(AppSessionState.SignedIn("alice"))
        val source =
            source(
                sessionProvider = sessionProvider,
                grantRepository = grantRepository,
                onlineFactory = onlineFactory,
                offlineFactory = offlineFactory,
            )
        source.open(
            DataSpec.Builder().setUri(PlaybackMediaUri.forTrack(offlineTrack.trackId)).build(),
        )

        sessionProvider.signIn("bob")
        val failure =
            runCatching {
                source.read(ByteArray(1), 0, 1)
            }.exceptionOrNull()

        assertThat(failure).isInstanceOf(IOException::class.java)
        assertThat(grantRepository.requestCount).isEqualTo(0)
    }

    private fun source(
        ownerState: AppSessionState,
        grantRepository: PlaybackGrantRepository,
        onlineFactory: RecordingDataSourceFactory,
        offlineFactory: RecordingDataSourceFactory,
    ): DataSource = source(
        sessionProvider = FakeSessionProvider(ownerState),
        grantRepository = grantRepository,
        onlineFactory = onlineFactory,
        offlineFactory = offlineFactory,
    )

    private fun source(
        sessionProvider: FakeSessionProvider,
        grantRepository: PlaybackGrantRepository,
        onlineFactory: RecordingDataSourceFactory,
        offlineFactory: RecordingDataSourceFactory,
    ): DataSource = GrantResolvingDataSourceFactory(
        onlineFactory = onlineFactory,
        offlineFactory = offlineFactory,
        grantRepository = grantRepository,
        offlineMediaStore = OfflineMediaStore(database.offlineTrackDao(), Lazy { cache }),
        sessionProvider = sessionProvider,
        sessionIdentityProvider = sessionProvider,
        sessionMutationCoordinator = SessionMutationCoordinator(),
    ).createDataSource()

    private fun offlineTrack(ownerUserId: String) = OfflineTrackEntity(
        ownerUserId = ownerUserId,
        trackId = TRACK_ID,
        title = "Track",
        artistNamesJson = "[]",
        albumTitle = null,
        artworkUrl = null,
        artworkCacheKey = null,
        durationMs = 1_000,
        cacheKey = "cache",
        contentLength = 128,
        downloadedAtEpochMs = 1,
    )

    private class FakeSessionProvider(initialState: AppSessionState) :
        AppSessionProvider,
        SessionIdentityProvider {
        private val generation = ServerRuntimeCoordinator().captureGeneration()
        private val mutableState = MutableStateFlow(initialState)
        private var identity =
            (initialState as? AppSessionState.SignedIn)?.let { state ->
                ActiveSessionIdentity(state.userId, "session-${state.userId}", generation)
            }
        override val sessionState: StateFlow<AppSessionState> = mutableState

        override fun activeIdentity(): ActiveSessionIdentity? = identity

        fun signIn(userId: String) {
            identity = ActiveSessionIdentity(userId, "session-$userId", generation)
            mutableState.value = AppSessionState.SignedIn(userId)
        }

        override suspend fun restoreSession() = Unit
    }

    private class UnavailableGrantRepository : PlaybackGrantRepository {
        var requestCount = 0

        override suspend fun get(
            trackId: String,
            preferredQuality: PreferredQuality,
            acceptedCodecs: List<String>,
            forceRefresh: Boolean,
        ): PlayerResult<PlaybackGrant> {
            requestCount += 1
            return PlayerResult.Failure(PlayerFailure.PlaybackUnavailable)
        }

        override fun invalidate(trackId: String) = Unit

        override fun clear() = Unit
    }

    private class AvailableGrantRepository(private val compatibleCodecFallbackEnabled: Boolean = false) :
        PlaybackGrantRepository {
        var requestCount = 0

        override fun isCompatibleCodecFallbackEnabled(trackId: String): Boolean = compatibleCodecFallbackEnabled

        override suspend fun get(
            trackId: String,
            preferredQuality: PreferredQuality,
            acceptedCodecs: List<String>,
            forceRefresh: Boolean,
        ): PlayerResult<PlaybackGrant> {
            requestCount += 1
            return PlayerResult.Success(
                PlaybackGrant(
                    trackId = trackId,
                    variantId = "variant",
                    selectedQuality = PreferredQuality.AUTO,
                    signedUrl = SIGNED_URL,
                    expiresAtEpochMillis = Long.MAX_VALUE,
                    mimeType = "audio/mpeg",
                    codec = "mp3",
                    container = "mp3",
                    bitrate = 320_000,
                    sampleRate = 44_100,
                    contentLength = 128,
                    checksumSha256 = null,
                    cacheKey = NETWORK_CACHE_KEY,
                ),
            )
        }

        override fun invalidate(trackId: String) = Unit

        override fun clear() = Unit
    }

    private class RecordingDataSourceFactory(private val openFailure: IOException? = null) : DataSource.Factory {
        var createCount = 0
        var lastOpenedSpec: DataSpec? = null

        override fun createDataSource(): DataSource {
            createCount += 1
            return object : DataSource {
                override fun addTransferListener(transferListener: TransferListener) = Unit

                override fun open(dataSpec: DataSpec): Long {
                    lastOpenedSpec = dataSpec
                    openFailure?.let { throw it }
                    return dataSpec.length.takeUnless { it == C.LENGTH_UNSET.toLong() } ?: 128
                }

                override fun read(buffer: ByteArray, offset: Int, length: Int): Int = C.RESULT_END_OF_INPUT

                override fun getUri(): Uri? = lastOpenedSpec?.uri

                override fun getResponseHeaders(): Map<String, List<String>> = emptyMap()

                override fun close() = Unit
            }
        }
    }

    private class FakeOfflineMediaCache : OfflineMediaCache {
        val cachedKeys = mutableSetOf<String>()

        override fun pin(cacheKey: String) = Unit

        override fun unpin(cacheKey: String) = Unit

        override fun promotePin(cacheKey: String) = Unit

        override fun isFullyCached(cacheKey: String, contentLength: Long): Boolean =
            contentLength > 0 && cacheKey in cachedKeys

        override suspend fun remove(cacheKey: String) {
            cachedKeys -= cacheKey
        }
    }

    private companion object {
        const val TRACK_ID = "00000000-0000-0000-0000-000000000001"
        const val SIGNED_URL = "https://media.example.test/playback"
        const val NETWORK_CACHE_KEY = "network-cache"
    }
}
