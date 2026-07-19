package com.xymusic.app.core.network.auth

import com.xymusic.app.core.common.IoDispatcher
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.network.model.ProblemCode
import com.xymusic.app.core.security.AccessToken
import com.xymusic.app.core.security.SessionTokens
import com.xymusic.app.core.security.TokenVault
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.core.session.SessionInvalidator
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.core.session.SessionStateController
import java.time.Clock
import java.util.concurrent.CancellationException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.TimeoutCancellationException
import kotlinx.coroutines.async
import kotlinx.coroutines.withTimeout

@Singleton
class SingleFlightRefreshCoordinator
@Inject
constructor(
    private val tokenVault: TokenVault,
    private val refreshTokenService: RefreshTokenService,
    private val refreshAttemptStore: RefreshAttemptStore,
    private val sessionStateController: SessionStateController,
    private val sessionInvalidator: SessionInvalidator,
    private val sessionMutationCoordinator: SessionMutationCoordinator,
    private val serverRuntimeCoordinator: ServerRuntimeCoordinator = ServerRuntimeCoordinator(),
    private val sessionIdentityProvider: SessionIdentityProvider =
        SessionIdentityProvider {
            tokenVault.read()?.let { tokens ->
                ActiveSessionIdentity(
                    userId = tokens.userId,
                    sessionId = tokens.sessionId,
                    serverGeneration = serverRuntimeCoordinator.captureGeneration(),
                )
            }
        },
    private val clock: Clock = Clock.systemUTC(),
    @IoDispatcher ioDispatcher: CoroutineDispatcher = Dispatchers.IO,
) {
    private val scope = CoroutineScope(SupervisorJob() + ioDispatcher)
    private val flightLock = Any()
    private var inFlight: RefreshFlight? = null
    private var cooldown: RefreshCooldown? = null

    suspend fun refresh(
        failedAccessToken: AccessToken,
        expectedIdentity: ActiveSessionIdentity? = currentIdentityOrNull(),
    ): RefreshOutcome {
        val expected = expectedIdentity ?: return RefreshOutcome.SessionUnavailable
        val currentTokens = currentTokensFor(expected) ?: return RefreshOutcome.SessionUnavailable
        if (currentTokens.accessToken != failedAccessToken) {
            return RefreshOutcome.Available(currentTokens)
        }
        val key = RefreshKey(expected, failedAccessToken)

        val decision =
            synchronized(flightLock) {
                inFlight
                    ?.takeIf {
                        it.key == key && !it.deferred.isCompleted && !it.deferred.isCancelled
                    }?.let { flight ->
                        flight.waiterCount += 1
                        return@synchronized RefreshDecision.Await(flight)
                    }

                val activeCooldown =
                    cooldown?.takeIf {
                        it.key == key && clock.millis() < it.untilEpochMillis
                    }
                if (activeCooldown != null) {
                    return@synchronized RefreshDecision.Immediate(activeCooldown.outcome)
                }

                val flightId = Any()
                val deferred =
                    scope.async(start = CoroutineStart.LAZY) {
                        var completedOutcome: RefreshOutcome? = null
                        try {
                            val outcome =
                                try {
                                    withTimeout(REFRESH_TIMEOUT_MS) {
                                        performRefresh(key)
                                    }
                                } catch (_: TimeoutCancellationException) {
                                    RefreshOutcome.TemporaryFailure
                                }
                            completedOutcome = outcome
                            outcome
                        } finally {
                            synchronized(flightLock) {
                                if (completedOutcome == RefreshOutcome.TemporaryFailure) {
                                    cooldown =
                                        RefreshCooldown(
                                            key = key,
                                            untilEpochMillis =
                                            clock.millis() +
                                                TEMPORARY_FAILURE_COOLDOWN_MS,
                                            outcome = RefreshOutcome.TemporaryFailure,
                                        )
                                } else if (completedOutcome != null) {
                                    cooldown = null
                                }
                                if (inFlight?.id === flightId) inFlight = null
                            }
                        }
                    }
                val flight =
                    RefreshFlight(
                        id = flightId,
                        key = key,
                        deferred = deferred,
                        waiterCount = 1,
                    )
                inFlight = flight
                RefreshDecision.Await(flight)
            }

        return when (decision) {
            is RefreshDecision.Immediate -> decision.outcome
            is RefreshDecision.Await -> {
                decision.flight.deferred.start()
                try {
                    decision.flight.deferred.await()
                } finally {
                    releaseWaiter(decision.flight)
                }
            }
        }
    }

    fun isCurrentSession(expectedIdentity: ActiveSessionIdentity, expectedTokens: SessionTokens): Boolean =
        serverRuntimeCoordinator.isCurrent(expectedIdentity.serverGeneration) &&
            currentIdentityOrNull() == expectedIdentity &&
            runCatching(tokenVault::read).getOrNull() == expectedTokens

    private suspend fun performRefresh(key: RefreshKey): RefreshOutcome {
        val expectedTokens =
            currentTokensFor(key.expectedIdentity)
                ?: return RefreshOutcome.SessionUnavailable
        if (expectedTokens.accessToken != key.failedAccessToken) {
            return RefreshOutcome.Available(expectedTokens)
        }

        return when (val result = requestRefreshedTokens(key.expectedIdentity, expectedTokens)) {
            is RefreshRequestResult.Failed -> result.outcome
            is RefreshRequestResult.Succeeded ->
                completeRefresh(key.expectedIdentity, expectedTokens, result.tokens)
        }
    }

    private suspend fun requestRefreshedTokens(
        expectedIdentity: ActiveSessionIdentity,
        expectedTokens: SessionTokens,
    ): RefreshRequestResult = try {
        RefreshRequestResult.Succeeded(
            refreshTokenService.refresh(
                RefreshTokenRequest(
                    refreshToken = expectedTokens.refreshToken,
                    expectedIdentity = expectedIdentity,
                ),
            ),
        )
    } catch (failure: CancellationException) {
        throw failure
    } catch (failure: TokenRefreshRejectedException) {
        val outcome =
            if (failure.domainError.invalidatesSession()) {
                invalidateExpectedSession(expectedIdentity, expectedTokens)
            } else {
                RefreshOutcome.TemporaryFailure
            }
        RefreshRequestResult.Failed(outcome)
    } catch (_: Exception) {
        RefreshRequestResult.Failed(RefreshOutcome.TemporaryFailure)
    }

    private suspend fun completeRefresh(
        expectedIdentity: ActiveSessionIdentity,
        expectedTokens: SessionTokens,
        refreshedTokens: SessionTokens,
    ): RefreshOutcome = if (
        refreshedTokens.userId != expectedTokens.userId ||
        refreshedTokens.sessionId != expectedTokens.sessionId
    ) {
        invalidateExpectedSession(expectedIdentity, expectedTokens)
    } else {
        commitRefreshedTokens(expectedIdentity, expectedTokens, refreshedTokens)
    }

    private suspend fun commitRefreshedTokens(
        expectedIdentity: ActiveSessionIdentity,
        expectedTokens: SessionTokens,
        refreshedTokens: SessionTokens,
    ): RefreshOutcome = sessionMutationCoordinator.mutate {
        if (!isExactExpectedSession(expectedIdentity, expectedTokens)) {
            return@mutate currentOutcomeFor(expectedIdentity)
        }
        try {
            tokenVault.write(refreshedTokens)
            clearRefreshAttemptBestEffort()
            sessionStateController.onSessionAvailable(refreshedTokens)
            RefreshOutcome.Available(refreshedTokens)
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: Exception) {
            RefreshOutcome.TemporaryFailure
        }
    }

    private suspend fun invalidateExpectedSession(
        expectedIdentity: ActiveSessionIdentity,
        expectedTokens: SessionTokens,
    ): RefreshOutcome = sessionMutationCoordinator.mutate {
        if (!isExactExpectedSession(expectedIdentity, expectedTokens)) {
            return@mutate currentOutcomeFor(expectedIdentity)
        }
        try {
            sessionInvalidator.invalidateSessionIfCurrent(expectedIdentity)
            RefreshOutcome.SessionUnavailable
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: Exception) {
            RefreshOutcome.TemporaryFailure
        }
    }

    private fun currentTokensFor(expectedIdentity: ActiveSessionIdentity): SessionTokens? {
        if (!serverRuntimeCoordinator.isCurrent(expectedIdentity.serverGeneration)) return null
        if (currentIdentityOrNull() != expectedIdentity) return null
        return runCatching(tokenVault::read).getOrNull()?.takeIf {
            it.userId == expectedIdentity.userId && it.sessionId == expectedIdentity.sessionId
        }
    }

    private fun isExactExpectedSession(
        expectedIdentity: ActiveSessionIdentity,
        expectedTokens: SessionTokens,
    ): Boolean = currentTokensFor(expectedIdentity) == expectedTokens

    private fun currentOutcomeFor(expectedIdentity: ActiveSessionIdentity): RefreshOutcome =
        currentTokensFor(expectedIdentity)
            ?.let { RefreshOutcome.Available(it) }
            ?: RefreshOutcome.SessionUnavailable

    private fun currentIdentityOrNull(): ActiveSessionIdentity? =
        runCatching(sessionIdentityProvider::activeIdentity).getOrNull()

    private fun releaseWaiter(flight: RefreshFlight) {
        val shouldCancel =
            synchronized(flightLock) {
                if (flight.waiterCount > 0) flight.waiterCount -= 1
                flight.waiterCount == 0 && !flight.deferred.isCompleted
            }
        if (shouldCancel) {
            flight.deferred.cancel(CancellationException("No refresh callers remain"))
        }
    }

    private fun clearRefreshAttemptBestEffort() {
        runCatching(refreshAttemptStore::clear)
    }

    private fun DomainError.invalidatesSession(): Boolean = when (this) {
        is DomainError.Authentication,
        is DomainError.PermissionDenied,
        -> true

        is DomainError.Conflict -> reason == ProblemCode.IdempotencyKeyReused
        else -> false
    }

    private data class RefreshKey(val expectedIdentity: ActiveSessionIdentity, val failedAccessToken: AccessToken)

    private data class RefreshFlight(
        val id: Any,
        val key: RefreshKey,
        val deferred: Deferred<RefreshOutcome>,
        var waiterCount: Int,
    )

    private data class RefreshCooldown(val key: RefreshKey, val untilEpochMillis: Long, val outcome: RefreshOutcome)

    private sealed interface RefreshDecision {
        data class Immediate(val outcome: RefreshOutcome) : RefreshDecision

        data class Await(val flight: RefreshFlight) : RefreshDecision
    }

    private sealed interface RefreshRequestResult {
        data class Succeeded(val tokens: SessionTokens) : RefreshRequestResult

        data class Failed(val outcome: RefreshOutcome) : RefreshRequestResult
    }

    private companion object {
        const val REFRESH_TIMEOUT_MS = 35_000L
        const val TEMPORARY_FAILURE_COOLDOWN_MS = 2_000L
    }
}

sealed interface RefreshOutcome {
    data class Available(val tokens: SessionTokens) : RefreshOutcome

    data object SessionUnavailable : RefreshOutcome

    data object TemporaryFailure : RefreshOutcome
}
