package com.xymusic.app.feature.player.data.media

import android.app.Application
import android.net.Uri
import androidx.media3.common.C
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.DataSpec
import androidx.media3.datasource.TransferListener
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.network.ServerGeneration
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.feature.player.domain.PlaybackGrant
import com.xymusic.app.feature.player.domain.PlaybackStreamProtocol
import com.xymusic.app.feature.player.domain.model.PreferredQuality
import java.io.IOException
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@UnstableApi
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class GrantResolvingDataSourceTest {
    @Test
    fun registeredProgressiveGrantRoutesToOnlineCacheWithTheGrantUrl() {
        val registry = PlaybackGrantRegistry()
        val onlineFactory = RecordingDataSourceFactory()
        val networkFactory = RecordingDataSourceFactory()
        val offlineFactory = RecordingDataSourceFactory()
        val grant = progressiveGrant()
        assertThat(registry.register(IDENTITY, grant)).isTrue()

        val source = source(registry, MutableIdentityProvider(IDENTITY), onlineFactory, networkFactory, offlineFactory)
        source.open(DataSpec.Builder().setUri(Uri.parse(grant.streamUrl)).setPosition(32).build())

        assertThat(onlineFactory.createCount).isEqualTo(1)
        assertThat(onlineFactory.lastOpenedSpec?.uri).isEqualTo(Uri.parse(grant.streamUrl))
        assertThat(onlineFactory.lastOpenedSpec?.key).isEqualTo(grant.cacheKey)
        assertThat(onlineFactory.lastOpenedSpec?.position).isEqualTo(32)
        assertThat(networkFactory.createCount).isEqualTo(0)
        assertThat(offlineFactory.createCount).isEqualTo(0)
    }

    @Test
    fun hlsPlaylistUsesNetworkFactoryAndSegmentsUseOnlineCacheFactory() {
        val registry = PlaybackGrantRegistry()
        val onlineFactory = RecordingDataSourceFactory()
        val networkFactory = RecordingDataSourceFactory()
        val offlineFactory = RecordingDataSourceFactory()
        val grant = hlsGrant()
        assertThat(registry.register(IDENTITY, grant)).isTrue()
        val source = source(registry, MutableIdentityProvider(IDENTITY), onlineFactory, networkFactory, offlineFactory)

        source.open(DataSpec.Builder().setUri(Uri.parse(grant.streamUrl)).build())
        source.close()
        source.open(
            DataSpec
                .Builder()
                .setUri(Uri.parse("$HLS_BASE/hls/segment_000001.m4s?ticket=$TICKET&cacheKey=${grant.cacheKey}"))
                .build(),
        )

        assertThat(networkFactory.createCount).isEqualTo(1)
        assertThat(networkFactory.lastOpenedSpec?.uri).isEqualTo(Uri.parse(grant.streamUrl))
        assertThat(onlineFactory.createCount).isEqualTo(1)
        assertThat(onlineFactory.lastOpenedSpec?.key).isEqualTo("${grant.cacheKey}:hls:segment_000001.m4s")
        assertThat(offlineFactory.createCount).isEqualTo(0)
    }

    @Test
    fun offlineUriRoutesDirectlyToReadOnlyCacheWithoutAGrant() {
        val registry = PlaybackGrantRegistry()
        val onlineFactory = RecordingDataSourceFactory()
        val networkFactory = RecordingDataSourceFactory()
        val offlineFactory = RecordingDataSourceFactory()
        val uri = PlaybackOfflineUri.forTrack(TRACK_ID, OFFLINE_CACHE_KEY)
        val source = source(registry, MutableIdentityProvider(IDENTITY), onlineFactory, networkFactory, offlineFactory)

        source.open(DataSpec.Builder().setUri(uri).setPosition(96).build())

        assertThat(offlineFactory.createCount).isEqualTo(1)
        assertThat(offlineFactory.lastOpenedSpec?.uri).isEqualTo(uri)
        assertThat(offlineFactory.lastOpenedSpec?.key).isEqualTo(OFFLINE_CACHE_KEY)
        assertThat(offlineFactory.lastOpenedSpec?.position).isEqualTo(96)
        assertThat(onlineFactory.createCount).isEqualTo(0)
        assertThat(networkFactory.createCount).isEqualTo(0)
    }

    @Test
    fun anUnregisteredPlaybackUrlIsRejectedBeforeAnyFactoryIsOpened() {
        val onlineFactory = RecordingDataSourceFactory()
        val networkFactory = RecordingDataSourceFactory()
        val offlineFactory = RecordingDataSourceFactory()
        val source = source(
            registry = PlaybackGrantRegistry(),
            identityProvider = MutableIdentityProvider(IDENTITY),
            onlineFactory = onlineFactory,
            networkFactory = networkFactory,
            offlineFactory = offlineFactory,
        )

        val failure = runCatching {
            source.open(DataSpec.Builder().setUri(Uri.parse(PROGRESSIVE_URL)).build())
        }.exceptionOrNull()

        assertThat(failure).isInstanceOf(IOException::class.java)
        assertThat(onlineFactory.createCount).isEqualTo(0)
        assertThat(networkFactory.createCount).isEqualTo(0)
        assertThat(offlineFactory.createCount).isEqualTo(0)
    }

    @Test
    fun aGrantCannotBeOpenedByAnotherSessionIdentity() {
        val registry = PlaybackGrantRegistry()
        val onlineFactory = RecordingDataSourceFactory()
        val networkFactory = RecordingDataSourceFactory()
        val offlineFactory = RecordingDataSourceFactory()
        val grant = progressiveGrant()
        assertThat(registry.register(IDENTITY, grant)).isTrue()
        val source = source(
            registry = registry,
            identityProvider = MutableIdentityProvider(OTHER_IDENTITY),
            onlineFactory = onlineFactory,
            networkFactory = networkFactory,
            offlineFactory = offlineFactory,
        )

        val failure = runCatching {
            source.open(DataSpec.Builder().setUri(Uri.parse(grant.streamUrl)).build())
        }.exceptionOrNull()

        assertThat(failure).isInstanceOf(IOException::class.java)
        assertThat(onlineFactory.createCount).isEqualTo(0)
        assertThat(networkFactory.createCount).isEqualTo(0)
        assertThat(offlineFactory.createCount).isEqualTo(0)
    }

    @Test
    fun switchingIdentityAfterOpenStopsFurtherReads() {
        val registry = PlaybackGrantRegistry()
        val identityProvider = MutableIdentityProvider(IDENTITY)
        val grant = progressiveGrant()
        assertThat(registry.register(IDENTITY, grant)).isTrue()
        val source = source(
            registry = registry,
            identityProvider = identityProvider,
            onlineFactory = RecordingDataSourceFactory(),
            networkFactory = RecordingDataSourceFactory(),
            offlineFactory = RecordingDataSourceFactory(),
        )
        source.open(DataSpec.Builder().setUri(Uri.parse(grant.streamUrl)).build())

        identityProvider.identity = OTHER_IDENTITY
        val failure = runCatching { source.read(ByteArray(1), 0, 1) }.exceptionOrNull()

        assertThat(failure).isInstanceOf(IOException::class.java)
    }

    private fun source(
        registry: PlaybackGrantRegistry,
        identityProvider: MutableIdentityProvider,
        onlineFactory: RecordingDataSourceFactory,
        networkFactory: RecordingDataSourceFactory,
        offlineFactory: RecordingDataSourceFactory,
    ): DataSource = GrantResolvingDataSourceFactory(
        onlineFactory = onlineFactory,
        networkFactory = networkFactory,
        offlineFactory = offlineFactory,
        grantRegistry = registry,
        sessionIdentityProvider = identityProvider,
    ).createDataSource()

    private class MutableIdentityProvider(var identity: ActiveSessionIdentity?) : SessionIdentityProvider {
        override fun activeIdentity(): ActiveSessionIdentity? = identity
    }

    private class RecordingDataSourceFactory : DataSource.Factory {
        var createCount = 0
        var lastOpenedSpec: DataSpec? = null

        override fun createDataSource(): DataSource {
            createCount += 1
            return object : DataSource {
                override fun addTransferListener(transferListener: TransferListener) = Unit

                override fun open(dataSpec: DataSpec): Long {
                    lastOpenedSpec = dataSpec
                    return dataSpec.length.takeUnless { it == C.LENGTH_UNSET.toLong() } ?: 128
                }

                override fun read(buffer: ByteArray, offset: Int, length: Int): Int = C.RESULT_END_OF_INPUT

                override fun getUri(): Uri? = lastOpenedSpec?.uri

                override fun getResponseHeaders(): Map<String, List<String>> = emptyMap()

                override fun close() = Unit
            }
        }
    }

    private fun progressiveGrant() = PlaybackGrant(
        trackId = TRACK_ID,
        sessionId = SESSION_ID,
        selectedQuality = PreferredQuality.STANDARD,
        streamUrl = PROGRESSIVE_URL,
        expiresAtEpochMillis = Long.MAX_VALUE,
        mimeType = "audio/mpeg",
        codec = "mp3",
        container = "mp3",
        bitrate = 320_000,
        sampleRate = 44_100,
        contentLength = 128,
        checksumSha256 = null,
        cacheKey = NETWORK_CACHE_KEY,
        streamProtocol = PlaybackStreamProtocol.PROGRESSIVE,
    )

    private fun hlsGrant() = progressiveGrant().let { grant ->
        PlaybackGrant(
            trackId = grant.trackId,
            sessionId = grant.sessionId,
            selectedQuality = grant.selectedQuality,
            streamUrl = HLS_PLAYLIST_URL,
            expiresAtEpochMillis = grant.expiresAtEpochMillis,
            mimeType = "audio/mp4",
            codec = "aac",
            container = "mp4",
            bitrate = grant.bitrate,
            sampleRate = grant.sampleRate,
            contentLength = null,
            checksumSha256 = null,
            cacheKey = grant.cacheKey,
            streamProtocol = PlaybackStreamProtocol.HLS,
        )
    }

    private companion object {
        const val TRACK_ID = "00000000-0000-0000-0000-000000000001"
        const val SESSION_ID = "10000000-0000-0000-0000-000000000001"
        const val OTHER_SESSION_ID = "10000000-0000-0000-0000-000000000002"
        const val TICKET = "ticket-value"
        const val NETWORK_CACHE_KEY = "network-cache"
        const val OFFLINE_CACHE_KEY = "offline-cache"
        const val BASE = "https://media.example.test/api/v1/playback/streams/$SESSION_ID"
        const val HLS_BASE = BASE
        const val PROGRESSIVE_URL = "$BASE?ticket=$TICKET"
        const val HLS_PLAYLIST_URL = "$BASE/index.m3u8?ticket=$TICKET"

        val IDENTITY = ActiveSessionIdentity(
            userId = "alice",
            sessionId = SESSION_ID,
            serverGeneration = ServerGeneration(0),
        )
        val OTHER_IDENTITY = ActiveSessionIdentity(
            userId = "bob",
            sessionId = OTHER_SESSION_ID,
            serverGeneration = ServerGeneration(0),
        )
    }
}
