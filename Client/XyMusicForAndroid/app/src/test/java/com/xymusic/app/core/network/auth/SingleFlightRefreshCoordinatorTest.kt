package com.xymusic.app.core.network.auth

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.network.model.ProblemCode
import com.xymusic.app.core.security.AccessToken
import com.xymusic.app.core.security.RefreshToken
import com.xymusic.app.core.security.SessionTokens
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.core.session.SessionInvalidator
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.data.network.BearerTokenInterceptor
import com.xymusic.app.data.network.SessionRequestContextBinder
import com.xymusic.app.data.network.SessionRequestContextCallFactory
import com.xymusic.app.data.network.SessionRequestContextValidationInterceptor
import com.xymusic.app.data.network.auth.RefreshingAuthenticator
import com.xymusic.app.support.InMemoryRefreshAttemptStore
import com.xymusic.app.support.InMemoryServerConfigRepository
import com.xymusic.app.support.InMemoryTokenVault
import com.xymusic.app.support.RecordingSessionInvalidator
import com.xymusic.app.support.RecordingSessionStateController
import java.io.IOException
import java.util.concurrent.CountDownLatch
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class SingleFlightRefreshCoordinatorTest {
    @Test
    fun unauthorizedRequestIsNotReplayedAfterSessionChangesDuringRefresh() = runTest {
        val server = MockWebServer()
        server.dispatcher =
            object : Dispatcher() {
                override fun dispatch(request: RecordedRequest): MockResponse = when (
                    request.getHeader("Authorization")
                ) {
                    "Bearer access-a" -> MockResponse().setResponseCode(401)
                    else -> MockResponse().setResponseCode(200).setBody("unexpected replay")
                }
            }
        server.start()
        try {
            val runtime =
                com.xymusic.app.core.network
                    .ServerRuntimeCoordinator()
            val sessionA =
                tokens(
                    accessToken = "access-a",
                    refreshToken = "refresh-a",
                    userId = "user-a",
                    sessionId = "session-a",
                )
            val sessionB =
                tokens(
                    accessToken = "access-b",
                    refreshToken = "refresh-b",
                    userId = "user-b",
                    sessionId = "session-b",
                )
            val vault = InMemoryTokenVault(sessionA)
            val identity =
                AtomicReference(
                    ActiveSessionIdentity(
                        sessionA.userId,
                        sessionA.sessionId,
                        runtime.captureGeneration(),
                    ),
                )
            val identityProvider = SessionIdentityProvider(identity::get)
            val refreshStarted = CountDownLatch(1)
            val allowRefreshToFinish = CountDownLatch(1)
            val refreshCount = AtomicInteger()
            val coordinator =
                SingleFlightRefreshCoordinator(
                    tokenVault = vault,
                    refreshTokenService =
                    RefreshTokenService {
                        refreshCount.incrementAndGet()
                        refreshStarted.countDown()
                        allowRefreshToFinish.await()
                        tokens(
                            accessToken = "new-access-a",
                            refreshToken = "new-refresh-a",
                            userId = "user-a",
                            sessionId = "session-a",
                        )
                    },
                    refreshAttemptStore = InMemoryRefreshAttemptStore(),
                    sessionStateController = RecordingSessionStateController(),
                    sessionInvalidator = SessionInvalidator { },
                    sessionMutationCoordinator = SessionMutationCoordinator(),
                    serverRuntimeCoordinator = runtime,
                    sessionIdentityProvider = identityProvider,
                )
            val serverRepository = InMemoryServerConfigRepository.from(server.url("/"))
            val binder =
                SessionRequestContextBinder(
                    vault,
                    identityProvider,
                    runtime,
                    serverRepository,
                )
            val client =
                OkHttpClient
                    .Builder()
                    .addInterceptor(
                        BearerTokenInterceptor(
                            tokenVault = vault,
                            serverConfigRepository = serverRepository,
                            serverRuntimeCoordinator = runtime,
                            sessionIdentityProvider = identityProvider,
                        ),
                    ).addNetworkInterceptor(
                        SessionRequestContextValidationInterceptor(
                            tokenVault = vault,
                            sessionIdentityProvider = identityProvider,
                            serverRuntimeCoordinator = runtime,
                        ),
                    ).authenticator(RefreshingAuthenticator(coordinator))
                    .build()
            val callFactory = SessionRequestContextCallFactory(client, binder)
            val responseCode =
                async(Dispatchers.IO) {
                    callFactory
                        .newCall(Request.Builder().url(server.url("/library")).build())
                        .execute()
                        .use { it.code }
                }

            refreshStarted.await()
            vault.write(sessionB)
            identity.set(
                ActiveSessionIdentity(
                    sessionB.userId,
                    sessionB.sessionId,
                    runtime.captureGeneration(),
                ),
            )
            allowRefreshToFinish.countDown()

            assertThat(responseCode.await()).isEqualTo(401)
            assertThat(server.requestCount).isEqualTo(1)
            assertThat(refreshCount.get()).isEqualTo(1)
            assertThat(vault.read()).isEqualTo(sessionB)
        } finally {
            server.shutdown()
        }
    }

    @Test
    fun inFlightRefreshDoesNotHoldMutationLockAndCancellationStopsTheFlight() = runTest {
        val dispatcher = StandardTestDispatcher(testScheduler)
        val mutationCoordinator = SessionMutationCoordinator()
        val refreshStarted = CompletableDeferred<Unit>()
        val refreshCancelled = CompletableDeferred<Unit>()
        val coordinator =
            SingleFlightRefreshCoordinator(
                tokenVault = InMemoryTokenVault(tokens("old-access", "old-refresh")),
                refreshTokenService =
                RefreshTokenService {
                    refreshStarted.complete(Unit)
                    try {
                        awaitCancellation()
                    } finally {
                        refreshCancelled.complete(Unit)
                    }
                },
                refreshAttemptStore = InMemoryRefreshAttemptStore(),
                sessionStateController = RecordingSessionStateController(),
                sessionInvalidator = SessionInvalidator { },
                sessionMutationCoordinator = mutationCoordinator,
                ioDispatcher = dispatcher,
            )
        val refresh =
            backgroundScope.async(dispatcher) {
                coordinator.refresh(AccessToken.from("old-access"))
            }
        runCurrent()
        refreshStarted.await()

        val independentMutation =
            backgroundScope.async(dispatcher) {
                mutationCoordinator.mutate { true }
            }
        runCurrent()

        assertThat(independentMutation.await()).isTrue()
        refresh.cancel()
        runCurrent()
        assertThat(refreshCancelled.isCompleted).isTrue()
    }

    @Test
    fun concurrentUnauthorizedResponsesTriggerExactlyOneRefresh() = runTest {
        val server = MockWebServer()
        server.dispatcher =
            object : Dispatcher() {
                override fun dispatch(request: RecordedRequest): MockResponse = when (
                    request.getHeader("Authorization")
                ) {
                    "Bearer old-access" -> MockResponse().setResponseCode(401)
                    "Bearer new-access" -> MockResponse().setResponseCode(200).setBody("ok")
                    else -> MockResponse().setResponseCode(500)
                }
            }
        server.start()
        try {
            val vault = InMemoryTokenVault(tokens("old-access", "old-refresh"))
            val refreshCount = AtomicInteger()
            val refreshService =
                RefreshTokenService {
                    refreshCount.incrementAndGet()
                    Thread.sleep(100)
                    tokens("new-access", "new-refresh")
                }
            val coordinator =
                SingleFlightRefreshCoordinator(
                    tokenVault = vault,
                    refreshTokenService = refreshService,
                    refreshAttemptStore = InMemoryRefreshAttemptStore(),
                    sessionStateController = RecordingSessionStateController(),
                    sessionInvalidator = SessionInvalidator { },
                    sessionMutationCoordinator = SessionMutationCoordinator(),
                )
            val client =
                OkHttpClient
                    .Builder()
                    .addInterceptor(
                        BearerTokenInterceptor(
                            vault,
                            InMemoryServerConfigRepository.from(server.url("/")),
                        ),
                    ).authenticator(RefreshingAuthenticator(coordinator))
                    .build()

            val statusCodes =
                List(2) {
                    async(Dispatchers.IO) {
                        client
                            .newCall(Request.Builder().url(server.url("/library")).build())
                            .execute()
                            .use { it.code }
                    }
                }.awaitAll()

            assertThat(statusCodes).containsExactly(200, 200)
            assertThat(refreshCount.get()).isEqualTo(1)
            assertThat(vault.read()?.accessToken).isEqualTo(AccessToken.from("new-access"))
        } finally {
            server.shutdown()
        }
    }

    @Test
    fun rejectedRefreshClearsPersistedAndObservableSession() = runTest {
        val vault = InMemoryTokenVault(tokens("old-access", "old-refresh"))
        val stateController = RecordingSessionStateController()
        val attemptStore = InMemoryRefreshAttemptStore()
        val coordinator =
            SingleFlightRefreshCoordinator(
                tokenVault = vault,
                refreshTokenService =
                RefreshTokenService {
                    throw TokenRefreshRejectedException(
                        DomainError.Authentication(
                            detail = "Session revoked",
                            traceId = "trace",
                            reason = ProblemCode.SessionRevoked,
                        ),
                    )
                },
                refreshAttemptStore = attemptStore,
                sessionStateController = stateController,
                sessionInvalidator =
                RecordingSessionInvalidator(
                    vault,
                    attemptStore,
                    stateController,
                ),
                sessionMutationCoordinator = SessionMutationCoordinator(),
            )

        val outcome = coordinator.refresh(AccessToken.from("old-access"))

        assertThat(outcome).isEqualTo(RefreshOutcome.SessionUnavailable)
        assertThat(vault.read()).isNull()
        assertThat(vault.clearCount).isEqualTo(1)
        assertThat(attemptStore.clearCount).isEqualTo(1)
        assertThat(stateController.cleared).isTrue()
    }

    @Test
    fun rejectedRefreshReturnsTemporaryFailureWhenLocalInvalidationCannotStart() = runTest {
        val vault = InMemoryTokenVault(tokens("old-access", "old-refresh"))
        val coordinator =
            SingleFlightRefreshCoordinator(
                tokenVault = vault,
                refreshTokenService =
                RefreshTokenService {
                    throw TokenRefreshRejectedException(
                        DomainError.Authentication(
                            detail = "Session revoked",
                            traceId = "trace",
                            reason = ProblemCode.SessionRevoked,
                        ),
                    )
                },
                refreshAttemptStore = InMemoryRefreshAttemptStore(),
                sessionStateController = RecordingSessionStateController(),
                sessionInvalidator =
                SessionInvalidator {
                    throw IOException("cleanup marker unavailable")
                },
                sessionMutationCoordinator = SessionMutationCoordinator(),
            )

        val outcome = coordinator.refresh(AccessToken.from("old-access"))

        assertThat(outcome).isEqualTo(RefreshOutcome.TemporaryFailure)
        assertThat(vault.read()).isNotNull()
    }

    @Test
    fun transportFailureKeepsSessionForRetry() = runTest {
        val vault = InMemoryTokenVault(tokens("old-access", "old-refresh"))
        val stateController = RecordingSessionStateController()
        val attemptStore = InMemoryRefreshAttemptStore()
        val coordinator =
            SingleFlightRefreshCoordinator(
                tokenVault = vault,
                refreshTokenService = RefreshTokenService { throw IOException("offline") },
                refreshAttemptStore = attemptStore,
                sessionStateController = stateController,
                sessionInvalidator = SessionInvalidator { },
                sessionMutationCoordinator = SessionMutationCoordinator(),
            )

        val outcome = coordinator.refresh(AccessToken.from("old-access"))

        assertThat(outcome).isEqualTo(RefreshOutcome.TemporaryFailure)
        assertThat(vault.read()).isNotNull()
        assertThat(vault.clearCount).isEqualTo(0)
        assertThat(attemptStore.clearCount).isEqualTo(0)
        assertThat(stateController.cleared).isFalse()
    }

    @Test
    fun concurrentTransportFailuresShareOneFailedRefresh() = runTest {
        val vault = InMemoryTokenVault(tokens("old-access", "old-refresh"))
        val refreshCount = AtomicInteger()
        val coordinator =
            SingleFlightRefreshCoordinator(
                tokenVault = vault,
                refreshTokenService =
                RefreshTokenService {
                    refreshCount.incrementAndGet()
                    Thread.sleep(100)
                    throw IOException("offline")
                },
                refreshAttemptStore = InMemoryRefreshAttemptStore(),
                sessionStateController = RecordingSessionStateController(),
                sessionInvalidator = SessionInvalidator { },
                sessionMutationCoordinator = SessionMutationCoordinator(),
            )

        val outcomes =
            List(2) {
                async(Dispatchers.IO) { coordinator.refresh(AccessToken.from("old-access")) }
            }.awaitAll()

        assertThat(outcomes).containsExactly(
            RefreshOutcome.TemporaryFailure,
            RefreshOutcome.TemporaryFailure,
        )
        assertThat(refreshCount.get()).isEqualTo(1)
    }

    @Test
    fun temporaryFailureCooldownSuppressesImmediateRetry() = runTest {
        val vault = InMemoryTokenVault(tokens("old-access", "old-refresh"))
        val refreshCount = AtomicInteger()
        val coordinator =
            SingleFlightRefreshCoordinator(
                tokenVault = vault,
                refreshTokenService =
                RefreshTokenService {
                    refreshCount.incrementAndGet()
                    throw IOException("offline")
                },
                refreshAttemptStore = InMemoryRefreshAttemptStore(),
                sessionStateController = RecordingSessionStateController(),
                sessionInvalidator = SessionInvalidator { },
                sessionMutationCoordinator = SessionMutationCoordinator(),
            )

        assertThat(coordinator.refresh(AccessToken.from("old-access")))
            .isEqualTo(RefreshOutcome.TemporaryFailure)
        assertThat(coordinator.refresh(AccessToken.from("old-access")))
            .isEqualTo(RefreshOutcome.TemporaryFailure)

        assertThat(refreshCount.get()).isEqualTo(1)
    }

    @Test
    fun cancellationIsNotConvertedToTemporaryFailure() = runTest {
        val coordinator =
            SingleFlightRefreshCoordinator(
                tokenVault = InMemoryTokenVault(tokens("old-access", "old-refresh")),
                refreshTokenService =
                RefreshTokenService {
                    throw java.util.concurrent.CancellationException("cancelled")
                },
                refreshAttemptStore = InMemoryRefreshAttemptStore(),
                sessionStateController = RecordingSessionStateController(),
                sessionInvalidator = SessionInvalidator { },
                sessionMutationCoordinator = SessionMutationCoordinator(),
            )

        val failure =
            runCatching {
                coordinator.refresh(AccessToken.from("old-access"))
            }.exceptionOrNull()

        assertThat(failure).isInstanceOf(java.util.concurrent.CancellationException::class.java)
    }

    @Test
    fun refreshReturningAnotherSessionClearsTheCurrentSession() = runTest {
        val vault = InMemoryTokenVault(tokens("old-access", "old-refresh"))
        val stateController = RecordingSessionStateController()
        val attemptStore = InMemoryRefreshAttemptStore()
        val coordinator =
            SingleFlightRefreshCoordinator(
                tokenVault = vault,
                refreshTokenService =
                RefreshTokenService {
                    tokens("new-access", "new-refresh").copy(sessionId = "another-session")
                },
                refreshAttemptStore = attemptStore,
                sessionStateController = stateController,
                sessionInvalidator =
                RecordingSessionInvalidator(
                    vault,
                    attemptStore,
                    stateController,
                ),
                sessionMutationCoordinator = SessionMutationCoordinator(),
            )

        val outcome = coordinator.refresh(AccessToken.from("old-access"))

        assertThat(outcome).isEqualTo(RefreshOutcome.SessionUnavailable)
        assertThat(vault.read()).isNull()
        assertThat(stateController.cleared).isTrue()
    }

    @Test
    fun refreshSuccessDoesNotDependOnClearingStaleAttemptMetadata() = runTest {
        val vault = InMemoryTokenVault(tokens("old-access", "old-refresh"))
        val coordinator =
            SingleFlightRefreshCoordinator(
                tokenVault = vault,
                refreshTokenService = RefreshTokenService { tokens("new-access", "new-refresh") },
                refreshAttemptStore =
                object : RefreshAttemptStore {
                    override fun idempotencyKeyFor(refreshToken: RefreshToken): String = "unused"

                    override fun clear(): Unit = throw IOException("preferences unavailable")
                },
                sessionStateController = RecordingSessionStateController(),
                sessionInvalidator = SessionInvalidator { },
                sessionMutationCoordinator = SessionMutationCoordinator(),
            )

        val outcome = coordinator.refresh(AccessToken.from("old-access"))

        assertThat(outcome).isInstanceOf(RefreshOutcome.Available::class.java)
        assertThat(vault.read()?.accessToken).isEqualTo(AccessToken.from("new-access"))
    }

    @Test
    fun logoutMutationRunsAfterAnOlderRefreshAndWinsFinalState() = runTest {
        val vault = InMemoryTokenVault(tokens("old-access", "old-refresh"))
        val mutationCoordinator = SessionMutationCoordinator()
        val refreshStarted = CountDownLatch(1)
        val allowRefreshToFinish = CountDownLatch(1)
        val coordinator =
            SingleFlightRefreshCoordinator(
                tokenVault = vault,
                refreshTokenService =
                RefreshTokenService {
                    refreshStarted.countDown()
                    allowRefreshToFinish.await()
                    tokens("new-access", "new-refresh")
                },
                refreshAttemptStore = InMemoryRefreshAttemptStore(),
                sessionStateController = RecordingSessionStateController(),
                sessionInvalidator = SessionInvalidator { },
                sessionMutationCoordinator = mutationCoordinator,
            )

        val refresh =
            async(Dispatchers.IO) {
                coordinator.refresh(AccessToken.from("old-access"))
            }
        refreshStarted.await()
        val logout =
            async(Dispatchers.IO) {
                mutationCoordinator.mutate { vault.clear() }
            }
        allowRefreshToFinish.countDown()

        refresh.await()
        logout.await()
        assertThat(vault.read()).isNull()
    }

    private fun tokens(
        accessToken: String,
        refreshToken: String,
        userId: String = "user-1",
        sessionId: String = "session-1",
    ): SessionTokens = SessionTokens(
        userId = userId,
        sessionId = sessionId,
        accessToken = AccessToken.from(accessToken),
        accessTokenExpiresAtEpochMillis = 1_800_000_000_000,
        refreshToken = RefreshToken.from(refreshToken),
        refreshTokenExpiresAtEpochMillis = 1_900_000_000_000,
    )
}
