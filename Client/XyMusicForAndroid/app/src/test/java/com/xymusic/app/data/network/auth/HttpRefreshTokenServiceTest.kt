package com.xymusic.app.data.network.auth

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.network.ProblemMapper
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.network.StaleServerGenerationException
import com.xymusic.app.core.network.auth.RefreshAttemptStore
import com.xymusic.app.core.network.auth.RefreshTokenRequest
import com.xymusic.app.core.security.RefreshToken
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.data.network.ProblemResponseParser
import com.xymusic.app.data.network.ServerEndpointInterceptor
import com.xymusic.app.support.InMemoryServerConfigRepository
import java.io.IOException
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.TimeoutCancellationException
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import okhttp3.mockwebserver.MockWebServer
import org.junit.Test

class HttpRefreshTokenServiceTest {
    @Test
    fun transportRetryReusesTheExactIdempotencyKeyAndRequest() = runTest {
        val json = Json { ignoreUnknownKeys = true }
        val apiBaseUrl = "https://api.xymusic.example/".toHttpUrl()
        val runtime = ServerRuntimeCoordinator()
        val serverRepository = InMemoryServerConfigRepository.from(apiBaseUrl)
        val requests = mutableListOf<Request>()
        var attempt = 0
        val executor =
            AuthCallExecutor { request ->
                requests += request
                attempt += 1
                if (attempt == 1) throw IOException("response lost")
                successfulResponse(request)
            }
        var requestedKeyCount = 0
        val refreshAttemptStore =
            object : RefreshAttemptStore {
                override fun idempotencyKeyFor(refreshToken: RefreshToken): String {
                    requestedKeyCount += 1
                    return "fixed-refresh-key"
                }

                override fun clear() = Unit
            }
        val service =
            HttpRefreshTokenService(
                apiBaseUrl = apiBaseUrl,
                callExecutor = executor,
                refreshAttemptStore = refreshAttemptStore,
                problemResponseParser = ProblemResponseParser(json, ProblemMapper()),
                json = json,
                clock = Clock.fixed(Instant.parse("2026-07-10T00:00:00Z"), ZoneOffset.UTC),
                serverConfigRepository = serverRepository,
                serverRuntimeCoordinator = runtime,
            )

        val tokens = service.refresh(refreshRequest(runtime))

        assertThat(requests).hasSize(2)
        assertThat(requests[0]).isSameInstanceAs(requests[1])
        assertThat(requests.map { it.header("Idempotency-Key") }).containsExactly(
            "fixed-refresh-key",
            "fixed-refresh-key",
        )
        assertThat(requests.map { it.method }).containsExactly("POST", "POST")
        assertThat(requests.map { it.url.encodedPath }).containsExactly(
            "/api/v1/auth/refresh",
            "/api/v1/auth/refresh",
        )
        assertThat(requests.map { it.header("Authorization") }).containsExactly(null, null)
        assertThat(requests.map { it.body?.contentType().toString() }).containsExactly(
            "application/json; charset=utf-8",
            "application/json; charset=utf-8",
        )
        assertThat(requests.map { checkNotNull(it.body).bodyText() }).containsExactly(
            "{\"refreshToken\":\"old-refresh-token\"}",
            "{\"refreshToken\":\"old-refresh-token\"}",
        )
        assertThat(requestedKeyCount).isEqualTo(1)
        assertThat(tokens.accessToken.value).isEqualTo(NEW_ACCESS_TOKEN)
        assertThat(tokens.refreshToken.value).isEqualTo(NEW_REFRESH_TOKEN)
    }

    @Test
    fun totalTimeoutCancelsTheInFlightAttemptInsteadOfStartingAnUnboundedRetry() = runTest {
        val json = Json { ignoreUnknownKeys = true }
        val apiBaseUrl = "https://api.xymusic.example/".toHttpUrl()
        val runtime = ServerRuntimeCoordinator()
        val cancellationObserved = CompletableDeferred<Unit>()
        var attemptCount = 0
        val service =
            HttpRefreshTokenService(
                apiBaseUrl = apiBaseUrl,
                callExecutor =
                AuthCallExecutor {
                    attemptCount += 1
                    try {
                        awaitCancellation()
                    } finally {
                        cancellationObserved.complete(Unit)
                    }
                },
                refreshAttemptStore =
                object : RefreshAttemptStore {
                    override fun idempotencyKeyFor(refreshToken: RefreshToken): String = "fixed-refresh-key"

                    override fun clear() = Unit
                },
                problemResponseParser = ProblemResponseParser(json, ProblemMapper()),
                json = json,
                clock = Clock.fixed(Instant.parse("2026-07-10T00:00:00Z"), ZoneOffset.UTC),
                serverConfigRepository = InMemoryServerConfigRepository.from(apiBaseUrl),
                serverRuntimeCoordinator = runtime,
            )

        val failure =
            runCatching {
                service.refresh(refreshRequest(runtime))
            }.exceptionOrNull()

        assertThat(failure).isInstanceOf(TimeoutCancellationException::class.java)
        assertThat(attemptCount).isEqualTo(1)
        assertThat(cancellationObserved.isCompleted).isTrue()
    }

    @Test
    fun refreshBoundToPreviousServerIsRejectedWithoutRetryOrCredentialLeak() = runTest {
        val serverA = MockWebServer()
        val serverB = MockWebServer()
        serverA.start()
        serverB.start()
        val releaseExecution = CompletableDeferred<Unit>()
        try {
            val json = Json { ignoreUnknownKeys = true }
            val runtime = ServerRuntimeCoordinator()
            val serverRepository = InMemoryServerConfigRepository.from(serverA.url("/"))
            val expectedRequest = refreshRequest(runtime)
            val actualExecutor =
                OkHttpAuthCallExecutor(
                    OkHttpClient
                        .Builder()
                        .addInterceptor(ServerEndpointInterceptor(serverRepository, runtime))
                        .build(),
                )
            val requestWasBound = CompletableDeferred<Unit>()
            var attemptCount = 0
            val service =
                HttpRefreshTokenService(
                    apiBaseUrl = serverA.url("/"),
                    callExecutor =
                    AuthCallExecutor { request ->
                        attemptCount += 1
                        requestWasBound.complete(Unit)
                        releaseExecution.await()
                        actualExecutor.execute(request)
                    },
                    refreshAttemptStore =
                    object : RefreshAttemptStore {
                        override fun idempotencyKeyFor(refreshToken: RefreshToken): String = "fixed-refresh-key"

                        override fun clear() = Unit
                    },
                    problemResponseParser = ProblemResponseParser(json, ProblemMapper()),
                    json = json,
                    clock =
                    Clock.fixed(
                        Instant.parse("2026-07-10T00:00:00Z"),
                        ZoneOffset.UTC,
                    ),
                    serverConfigRepository = serverRepository,
                    serverRuntimeCoordinator = runtime,
                )
            val refresh =
                backgroundScope.async(Dispatchers.IO) {
                    runCatching { service.refresh(expectedRequest) }.exceptionOrNull()
                }
            requestWasBound.await()

            val switchingGeneration = runtime.beginSwitch()
            serverRepository.update(
                checkNotNull(
                    InMemoryServerConfigRepository.from(serverB.url("/")).currentEndpoint(),
                ),
            )
            runtime.finishSwitch(switchingGeneration)
            releaseExecution.complete(Unit)

            assertThat(refresh.await()).isInstanceOf(StaleServerGenerationException::class.java)
            assertThat(attemptCount).isEqualTo(1)
            assertThat(serverA.requestCount).isEqualTo(0)
            assertThat(serverB.requestCount).isEqualTo(0)
        } finally {
            releaseExecution.complete(Unit)
            serverA.shutdown()
            serverB.shutdown()
        }
    }

    private fun refreshRequest(runtime: ServerRuntimeCoordinator): RefreshTokenRequest = RefreshTokenRequest(
        refreshToken = RefreshToken.from("old-refresh-token"),
        expectedIdentity =
        ActiveSessionIdentity(
            userId = "user-1",
            sessionId = "session-1",
            serverGeneration = runtime.captureGeneration(),
        ),
    )

    private fun successfulResponse(request: Request): Response = Response
        .Builder()
        .request(request)
        .protocol(Protocol.HTTP_1_1)
        .code(200)
        .message("OK")
        .body(
            """
                {
                  "user": {
                    "id": "11111111-1111-4111-8111-111111111111",
                    "username": "alice_01",
                    "displayName": "Alice",
                    "bio": null,
                    "avatar": null,
                    "role": "USER",
                    "status": "ACTIVE",
                    "version": 1,
                    "createdAt": "2026-07-09T00:00:00Z",
                    "updatedAt": "2026-07-09T00:00:00Z"
                  },
                  "session": {
                    "id": "22222222-2222-4222-8222-222222222222",
                    "deviceName": "Test device",
                    "createdAt": "2026-07-09T00:00:00Z"
                  },
                  "tokens": {
                    "tokenType": "Bearer",
                    "accessToken": "$NEW_ACCESS_TOKEN",
                    "accessTokenExpiresAt": "2026-07-10T01:00:00Z",
                    "refreshToken": "$NEW_REFRESH_TOKEN",
                    "refreshTokenExpiresAt": "2026-08-10T00:00:00Z"
                  }
                }
            """.trimIndent().toResponseBody(),
        ).build()

    private fun okhttp3.RequestBody.bodyText(): String {
        val buffer = okio.Buffer()
        writeTo(buffer)
        return buffer.readUtf8()
    }

    private companion object {
        const val NEW_ACCESS_TOKEN = "new-access-token-12345678901234567890"
        const val NEW_REFRESH_TOKEN = "new-refresh-token-1234567890123456789"
    }
}
