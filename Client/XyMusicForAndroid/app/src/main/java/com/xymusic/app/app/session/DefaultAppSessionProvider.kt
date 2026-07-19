package com.xymusic.app.app.session

import com.xymusic.app.core.common.IoDispatcher
import com.xymusic.app.core.common.runCatchingPreservingCancellation
import com.xymusic.app.core.database.AccountDataCleaner
import com.xymusic.app.core.database.PendingAccountCleanupStore
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.network.auth.RefreshAttemptStore
import com.xymusic.app.core.security.SessionTokens
import com.xymusic.app.core.security.TokenVault
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.core.session.SessionInvalidator
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.core.session.SessionStateController
import com.xymusic.app.core.sync.PendingSyncScheduler
import java.time.Clock
import java.util.concurrent.CancellationException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext

@Singleton
class DefaultAppSessionProvider
@Inject
constructor(
    private val tokenVault: TokenVault,
    private val clock: Clock,
    private val accountDataCleaner: AccountDataCleaner,
    private val pendingAccountCleanupStore: PendingAccountCleanupStore,
    private val refreshAttemptStore: RefreshAttemptStore,
    private val pendingSyncScheduler: PendingSyncScheduler,
    @IoDispatcher private val ioDispatcher: CoroutineDispatcher,
    private val serverRuntimeCoordinator: ServerRuntimeCoordinator = ServerRuntimeCoordinator(),
    private val sessionMutationCoordinator: SessionMutationCoordinator = SessionMutationCoordinator(),
) : AppSessionProvider,
    SessionStateController,
    SessionInvalidator,
    SessionIdentityProvider {
    private val mutableSessionState = MutableStateFlow<AppSessionState>(AppSessionState.Loading)
    private val restoreMutex = Mutex()

    @Volatile
    private var activeOwnerUserId: String? = null

    @Volatile
    private var activeIdentity: ActiveSessionIdentity? = null

    override val sessionState: StateFlow<AppSessionState> = mutableSessionState.asStateFlow()

    override fun activeIdentity(): ActiveSessionIdentity? = activeIdentity

    override suspend fun restoreSession() = restoreMutex.withLock {
        sessionMutationCoordinator.mutate {
            if (mutableSessionState.value != AppSessionState.Loading) return@mutate
            val tokens =
                try {
                    withContext(ioDispatcher) { restorePersistedSession() }
                } catch (failure: CancellationException) {
                    throw failure
                } catch (_: Exception) {
                    null
                }
            if (tokens == null || tokens.isRefreshExpired(clock.millis())) {
                if (tokens == null) onSessionCleared() else invalidateSession(tokens.userId)
            } else {
                onSessionAvailable(tokens)
            }
        }
    }

    override suspend fun invalidateSession(ownerUserId: String?) {
        invalidateSession(ownerUserId, expectedIdentity = null)
    }

    override suspend fun invalidateSessionIfCurrent(expectedIdentity: ActiveSessionIdentity) {
        invalidateSession(expectedIdentity.userId, expectedIdentity)
    }

    private suspend fun invalidateSession(ownerUserId: String?, expectedIdentity: ActiveSessionIdentity?) {
        val failures = FirstInvalidationFailure()
        withContext(NonCancellable + ioDispatcher) {
            val context = resolveInvalidationContext(ownerUserId, expectedIdentity)
            persistCleanupMarker(context.ownerUserId)
            val tokenCleared = clearCurrentSession(context.clearsCurrentSession, failures)
            val cleanupSucceeded = clearOwnerData(context.ownerUserId, failures)
            removeCleanupMarkerIfComplete(
                context.ownerUserId,
                cleanupSucceeded,
                tokenCleared,
                failures,
            )
        }
        failures.throwIfPresent()
    }

    private fun resolveInvalidationContext(
        requestedOwnerUserId: String?,
        expectedIdentity: ActiveSessionIdentity?,
    ): InvalidationContext {
        val currentIdentity = activeIdentity
        val currentTokens = runCatching(tokenVault::read).getOrNull()
        val ownerUserId = requestedOwnerUserId ?: activeOwnerUserId ?: currentTokens?.userId
        val clearsCurrentSession =
            shouldClearCurrentSession(
                requestedOwnerUserId = requestedOwnerUserId,
                expectedIdentity = expectedIdentity,
                currentIdentity = currentIdentity,
                currentTokens = currentTokens,
            )
        return InvalidationContext(ownerUserId, clearsCurrentSession)
    }

    private fun shouldClearCurrentSession(
        requestedOwnerUserId: String?,
        expectedIdentity: ActiveSessionIdentity?,
        currentIdentity: ActiveSessionIdentity?,
        currentTokens: SessionTokens?,
    ): Boolean = when {
        expectedIdentity != null ->
            currentIdentity?.let { it == expectedIdentity }
                ?: currentTokens?.belongsTo(expectedIdentity)
                ?: false
        requestedOwnerUserId != null ->
            currentIdentity?.userId?.let { it == requestedOwnerUserId }
                ?: currentTokens?.userId?.let { it == requestedOwnerUserId }
                ?: false
        else -> currentIdentity != null || currentTokens != null
    }

    private fun persistCleanupMarker(ownerUserId: String?) {
        if (ownerUserId == null) return
        try {
            pendingAccountCleanupStore.add(ownerUserId)
        } catch (failure: Exception) {
            throw SessionInvalidationException(failure)
        }
    }

    private fun clearCurrentSession(clearsCurrentSession: Boolean, failures: FirstInvalidationFailure): Boolean {
        if (!clearsCurrentSession) return true
        onSessionCleared()
        val tokenClearResult = runCatching(tokenVault::clear)
        tokenClearResult.exceptionOrNull()?.let(failures::record)
        runCatching(refreshAttemptStore::clear)
        return tokenClearResult.isSuccess
    }

    private suspend fun clearOwnerData(ownerUserId: String?, failures: FirstInvalidationFailure): Boolean {
        if (ownerUserId == null) return true
        runCatching { pendingSyncScheduler.cancel(ownerUserId) }
        val cleanupResult = runCatching { accountDataCleaner.clear(ownerUserId) }
        cleanupResult.exceptionOrNull()?.let(failures::record)
        return cleanupResult.isSuccess
    }

    private fun removeCleanupMarkerIfComplete(
        ownerUserId: String?,
        cleanupSucceeded: Boolean,
        tokenCleared: Boolean,
        failures: FirstInvalidationFailure,
    ) {
        if (ownerUserId == null || !cleanupSucceeded || !tokenCleared) return
        runCatching { pendingAccountCleanupStore.remove(ownerUserId) }
            .exceptionOrNull()
            ?.let(failures::record)
    }

    private suspend fun restorePersistedSession(): SessionTokens? {
        val cleanupResults = retryPendingAccountCleanup()
        val pendingOwnersAtStartup = cleanupResults.keys
        val persistedTokens = tokenVault.read()
        val tokens = persistedTokens?.takeUnless { it.userId in pendingOwnersAtStartup }
        val pendingTokenCleared =
            if (persistedTokens?.userId in pendingOwnersAtStartup) {
                val cleared = runCatching(tokenVault::clear).isSuccess
                runCatching(refreshAttemptStore::clear)
                cleared
            } else {
                true
            }
        cleanupResults.forEach { (owner, cleanupSucceeded) ->
            val ownerTokenCleared = persistedTokens?.userId != owner || pendingTokenCleared
            if (cleanupSucceeded && ownerTokenCleared) {
                runCatching { pendingAccountCleanupStore.remove(owner) }
            }
        }
        return tokens
    }

    private suspend fun retryPendingAccountCleanup(): Map<String, Boolean> {
        val owners = pendingAccountCleanupStore.owners()
        return owners.associateWith { ownerUserId ->
            runCatching { pendingSyncScheduler.cancel(ownerUserId) }
            runCatchingPreservingCancellation { accountDataCleaner.clear(ownerUserId) }
                .isSuccess
        }
    }

    override fun onSessionAvailable(tokens: SessionTokens) {
        val ownerChanged = activeOwnerUserId != tokens.userId
        activeOwnerUserId?.takeIf { ownerChanged }?.let { owner ->
            runCatching { pendingSyncScheduler.cancel(owner) }
        }
        activeOwnerUserId = tokens.userId
        activeIdentity =
            ActiveSessionIdentity(
                userId = tokens.userId,
                sessionId = tokens.sessionId,
                serverGeneration = serverRuntimeCoordinator.captureGeneration(),
            )
        mutableSessionState.value = AppSessionState.SignedIn(tokens.userId)
        if (ownerChanged) runCatching { pendingSyncScheduler.schedule(tokens.userId) }
    }

    override fun onSessionCleared() {
        activeOwnerUserId?.let { owner -> runCatching { pendingSyncScheduler.cancel(owner) } }
        activeOwnerUserId = null
        activeIdentity = null
        mutableSessionState.value = AppSessionState.SignedOut
    }

    private fun SessionTokens.belongsTo(identity: ActiveSessionIdentity): Boolean =
        userId == identity.userId && sessionId == identity.sessionId

    private data class InvalidationContext(val ownerUserId: String?, val clearsCurrentSession: Boolean)

    private class FirstInvalidationFailure {
        private var failure: Throwable? = null

        fun record(candidate: Throwable) {
            if (failure == null) failure = candidate
        }

        fun throwIfPresent() {
            failure?.let { throw SessionInvalidationException(it) }
        }
    }

    private class SessionInvalidationException(cause: Throwable) :
        IllegalStateException("Unable to completely clear the local session", cause)
}
