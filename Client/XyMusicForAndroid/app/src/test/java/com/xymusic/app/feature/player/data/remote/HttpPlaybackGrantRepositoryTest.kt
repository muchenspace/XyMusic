package com.xymusic.app.feature.player.data.remote

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.network.ProblemMapper
import com.xymusic.app.core.network.ServerGeneration
import com.xymusic.app.core.network.ServerSynchronizedClock
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.data.network.ProblemResponseParser
import com.xymusic.app.feature.player.data.media.InMemoryPlaybackGrantStore
import com.xymusic.app.feature.player.data.media.PlaybackGrantKey
import com.xymusic.app.feature.player.data.media.PlaybackGrantRegistry
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.support.InMemoryServerConfigRepository
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import okhttp3.Headers
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.ResponseBody
import org.junit.Test
import retrofit2.Response

@OptIn(ExperimentalCoroutinesApi::class)
class HttpPlaybackGrantRepositoryTest {
    @Test
    fun getRetrievesAndCachesPlaybackGrant() = runTest {
        val trackId = "00000000-0000-0000-0000-000000000001"
        val api = RecordingPlaybackApi()
        val repository = repository(api)

        val first = repository.get(trackId)
        assertThat(first).isInstanceOf(PlayerResult.Success::class.java)
        assertThat(api.requests).hasSize(1)

        val second = repository.get(trackId)
        assertThat(second).isInstanceOf(PlayerResult.Success::class.java)
        assertThat(api.requests).hasSize(1)
    }

    @Test
    fun getWithForceRefreshBypassesCache() = runTest {
        val trackId = "00000000-0000-0000-0000-000000000001"
        val api = RecordingPlaybackApi()
        val repository = repository(api)

        assertThat(repository.get(trackId)).isInstanceOf(PlayerResult.Success::class.java)
        assertThat(api.requests).hasSize(1)

        assertThat(repository.get(trackId, forceRefresh = true)).isInstanceOf(PlayerResult.Success::class.java)
        assertThat(api.requests).hasSize(2)
    }

    @Test
    fun invalidateTrackRemovesCachedGrant() = runTest {
        val trackId = "00000000-0000-0000-0000-000000000001"
        val api = RecordingPlaybackApi()
        val repository = repository(api)

        assertThat(repository.get(trackId)).isInstanceOf(PlayerResult.Success::class.java)
        assertThat(api.requests).hasSize(1)

        repository.invalidate(trackId)

        assertThat(repository.get(trackId)).isInstanceOf(PlayerResult.Success::class.java)
        assertThat(api.requests).hasSize(2)
    }

    @Test
    fun differentTracksCanRequestGrantsConcurrently() = runTest {
        val firstTrackId = "00000000-0000-0000-0000-000000000001"
        val secondTrackId = "00000000-0000-0000-0000-000000000002"
        assertThat(grantStripe(firstTrackId)).isNotEqualTo(grantStripe(secondTrackId))
        val api = BlockingPlaybackApi()
        val repository = repository(api)

        val first = async { repository.get(firstTrackId, forceRefresh = true) }
        val second = async { repository.get(secondTrackId, forceRefresh = true) }
        runCurrent()

        assertThat(api.startedTrackIds).containsExactly(firstTrackId, secondTrackId)
        api.allowResponses.complete(Unit)
        assertThat(first.await()).isInstanceOf(PlayerResult.Success::class.java)
        assertThat(second.await()).isInstanceOf(PlayerResult.Success::class.java)
    }

    @Test
    fun clearPreventsAnInFlightGrantFromRepopulatingTheCache() = runTest {
        val trackId = "00000000-0000-0000-0000-000000000001"
        val api = BlockingPlaybackApi()
        val store = InMemoryPlaybackGrantStore()
        val repository = repository(api, store)
        val request = async { repository.get(trackId, forceRefresh = true) }
        runCurrent()
        assertThat(api.startedTrackIds).containsExactly(trackId)

        repository.clear()
        api.allowResponses.complete(Unit)

        assertThat(request.await()).isInstanceOf(PlayerResult.Failure::class.java)
        val retry = repository.get(trackId, forceRefresh = false)
        assertThat(retry).isInstanceOf(PlayerResult.Success::class.java)
        assertThat(api.startedTrackIds).containsExactly(trackId, trackId).inOrder()
    }

    @Test
    fun httpsServerRejectsCleartextPlaybackGrant() = runTest {
        val api = BlockingPlaybackApi(grantUrlScheme = "http")
        api.allowResponses.complete(Unit)

        val result = repository(api).get("00000000-0000-0000-0000-000000000001", forceRefresh = true)
        assertThat(result).isInstanceOf(PlayerResult.Failure::class.java)
    }

    @Test
    fun explicitlyConfiguredHttpServerAllowsCleartextPlaybackGrant() = runTest {
        val api = BlockingPlaybackApi(grantUrlScheme = "http")
        api.allowResponses.complete(Unit)

        val result = repository(api, serverBaseUrl = "http://music.example/").get(
            "00000000-0000-0000-0000-000000000001",
            forceRefresh = true,
        )
        assertThat(result).isInstanceOf(PlayerResult.Success::class.java)
    }

    @Test
    fun fastDeviceClockUsesServerDateForGrantLifetime() = runTest {
        val deviceNow = Instant.parse("2026-01-01T00:07:34Z")
        val clock = ServerSynchronizedClock(Clock.fixed(deviceNow, ZoneOffset.UTC))
        val api = BlockingPlaybackApi(
            serverDate = "Thu, 1 Jan 2026 00:00:00 GMT",
            grantExpiresAt = "2026-01-01T00:05:00Z",
        )
        api.allowResponses.complete(Unit)

        val result = repository(api, clock = clock).get(
            "00000000-0000-0000-0000-000000000001",
            forceRefresh = true,
        )

        val grant = (result as PlayerResult.Success).value
        assertThat(clock.millis()).isEqualTo(Instant.parse("2026-01-01T00:00:00Z").toEpochMilli())
        assertThat(grant.expiresAtEpochMillis)
            .isEqualTo(Instant.parse("2026-01-01T00:05:00Z").toEpochMilli())
    }

    @Test
    fun slowDeviceClockUsesServerDateForGrantLifetime() = runTest {
        val deviceNow = Instant.parse("2025-12-31T23:52:26Z")
        val clock = ServerSynchronizedClock(Clock.fixed(deviceNow, ZoneOffset.UTC))
        val api = BlockingPlaybackApi(
            serverDate = "Thu, 1 Jan 2026 00:00:00 GMT",
            grantExpiresAt = "2026-01-01T00:05:00Z",
        )
        api.allowResponses.complete(Unit)

        val result = repository(api, clock = clock).get(
            "00000000-0000-0000-0000-000000000001",
            forceRefresh = true,
        )

        val grant = (result as PlayerResult.Success).value
        assertThat(clock.millis()).isEqualTo(Instant.parse("2026-01-01T00:00:00Z").toEpochMilli())
        assertThat(grant.expiresAtEpochMillis)
            .isEqualTo(Instant.parse("2026-01-01T00:05:00Z").toEpochMilli())
    }

    @Test
    fun serverLifetimeBelowMinimumIsRejectedDespiteDeviceClockSkew() = runTest {
        val api = BlockingPlaybackApi(
            serverDate = "Thu, 1 Jan 2026 00:00:00 GMT",
            grantExpiresAt = "2026-01-01T00:00:04Z",
        )
        api.allowResponses.complete(Unit)

        val result = repository(
            api,
            clock = ServerSynchronizedClock(
                Clock.fixed(Instant.parse("2025-12-31T23:00:00Z"), ZoneOffset.UTC),
            ),
        ).get(
            "00000000-0000-0000-0000-000000000001",
            forceRefresh = true,
        )

        assertThat(result).isInstanceOf(PlayerResult.Failure::class.java)
    }

    @Test
    fun sessionChangeRejectsAnInFlightGrantWithoutRelyingOnServiceCleanup() = runTest {
        val trackId = "00000000-0000-0000-0000-000000000001"
        val api = BlockingPlaybackApi()
        val identities = MutableSessionIdentityProvider(TEST_IDENTITY)
        val repository = repository(api, sessionIdentityProvider = identities)
        val request = async { repository.get(trackId, forceRefresh = true) }
        runCurrent()

        identities.identity = TEST_IDENTITY.copy(sessionId = "30000000-0000-0000-0000-000000000002")
        api.allowResponses.complete(Unit)

        assertThat(request.await()).isInstanceOf(PlayerResult.Failure::class.java)
        assertThat(repository.get(trackId, forceRefresh = false)).isInstanceOf(PlayerResult.Success::class.java)
        assertThat(api.startedTrackIds).containsExactly(trackId, trackId).inOrder()
    }

    private fun repository(
        api: PlaybackApi,
        store: InMemoryPlaybackGrantStore = InMemoryPlaybackGrantStore(),
        serverBaseUrl: String = "https://music.example/",
        sessionIdentityProvider: SessionIdentityProvider = SessionIdentityProvider { TEST_IDENTITY },
        clock: ServerSynchronizedClock =
            ServerSynchronizedClock(
                Clock.fixed(Instant.parse("2026-01-01T00:00:00Z"), ZoneOffset.UTC),
            ),
        grantRegistry: PlaybackGrantRegistry = PlaybackGrantRegistry(),
    ) = HttpPlaybackGrantRepository(
        api = api,
        store = store,
        problemResponseParser = ProblemResponseParser(Json, ProblemMapper()),
        serverConfigRepository =
            InMemoryServerConfigRepository.from(
                serverBaseUrl.toHttpUrl(),
            ),
        sessionIdentityProvider = sessionIdentityProvider,
        clock = clock,
        grantRegistry = grantRegistry,
    )

    private fun grantStripe(trackId: String): Int {
        val key =
            PlaybackGrantKey(
                ownerUserId = TEST_IDENTITY.userId,
                sessionId = TEST_IDENTITY.sessionId,
                serverGeneration = TEST_IDENTITY.serverGeneration.value,
                trackId = trackId,
            )
        return (key.hashCode() and Int.MAX_VALUE) % 32
    }

    private companion object {
        val TEST_IDENTITY =
            ActiveSessionIdentity(
                userId = "20000000-0000-0000-0000-000000000001",
                sessionId = "30000000-0000-0000-0000-000000000001",
                serverGeneration = ServerGeneration(0),
            )
    }
}

private class RecordingPlaybackApi : PlaybackApi {
    val requests = mutableListOf<PlaybackRequestDto>()
    override suspend fun grant(trackId: String, request: PlaybackRequestDto): Response<PlaybackGrantDto> {
        requests += request
        return Response.success(
            PlaybackGrantDto(
                trackId = trackId,
                streamUrl = "https://music.example/api/v1/playback/streams/$trackId?ticket=ticket",
                expiresAt = "2026-01-01T00:10:00Z",
                mimeType = "audio/mp4",
                codec = "aac",
                container = "m4a",
                bitrate = 256_000,
                sampleRate = 48_000,
                contentLength = 1_024,
                durationMs = 180_000,
            ),
        )
    }

    override suspend fun recordHistory(
        trackId: String,
        idempotencyKey: String,
        request: RecordPlaybackRequestDto,
    ): Response<ResponseBody> = error("Not used")
}

private class MutableSessionIdentityProvider(var identity: ActiveSessionIdentity?) : SessionIdentityProvider {
    override fun activeIdentity(): ActiveSessionIdentity? = identity
}

private class BlockingPlaybackApi(
    private val grantUrlScheme: String = "https",
    private val serverDate: String? = null,
    private val grantExpiresAt: String = "2026-01-01T00:10:00Z",
) : PlaybackApi {
    val startedTrackIds = mutableListOf<String>()
    val requests = mutableListOf<PlaybackRequestDto>()
    val allowResponses = CompletableDeferred<Unit>()

    override suspend fun grant(trackId: String, request: PlaybackRequestDto): Response<PlaybackGrantDto> {
        startedTrackIds += trackId
        requests += request
        allowResponses.await()
        val body =
            PlaybackGrantDto(
                trackId = trackId,
                streamUrl = "$grantUrlScheme://music.example/api/v1/playback/streams/$trackId?ticket=ticket",
                expiresAt = grantExpiresAt,
                mimeType = "audio/mp4",
                codec = "aac",
                container = "m4a",
                bitrate = 256_000,
                sampleRate = 48_000,
                contentLength = 1_024,
                durationMs = 180_000,
            )
        return if (serverDate == null) {
            Response.success(body)
        } else {
            Response.success(
                body,
                Headers.Builder().add("Date", serverDate).build(),
            )
        }
    }

    override suspend fun recordHistory(
        trackId: String,
        idempotencyKey: String,
        request: RecordPlaybackRequestDto,
    ): Response<ResponseBody> = error("Not used")
}
