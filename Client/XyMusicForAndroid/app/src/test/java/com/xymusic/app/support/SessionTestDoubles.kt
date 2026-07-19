package com.xymusic.app.support

import com.xymusic.app.core.database.AccountDataCleaner
import com.xymusic.app.core.database.PendingAccountCleanupStore
import com.xymusic.app.core.network.auth.RefreshAttemptStore
import com.xymusic.app.core.security.RefreshToken
import com.xymusic.app.core.security.SessionTokens
import com.xymusic.app.core.security.TokenVault
import com.xymusic.app.core.session.SessionInvalidator
import com.xymusic.app.core.session.SessionStateController

class InMemoryTokenVault(initialTokens: SessionTokens? = null) : TokenVault {
    private var storedTokens: SessionTokens? = initialTokens

    var clearCount: Int = 0
        private set

    var readCount: Int = 0
        private set

    @Synchronized
    override fun read(): SessionTokens? {
        readCount += 1
        return storedTokens
    }

    @Synchronized
    override fun write(tokens: SessionTokens) {
        storedTokens = tokens
    }

    @Synchronized
    override fun clear() {
        storedTokens = null
        clearCount += 1
    }
}

class RecordingSessionStateController : SessionStateController {
    var availableTokens: SessionTokens? = null
        private set
    var cleared: Boolean = false
        private set

    override fun onSessionAvailable(tokens: SessionTokens) {
        availableTokens = tokens
        cleared = false
    }

    override fun onSessionCleared() {
        availableTokens = null
        cleared = true
    }
}

class RecordingSessionInvalidator(
    private val tokenVault: TokenVault,
    private val refreshAttemptStore: RefreshAttemptStore,
    private val stateController: SessionStateController,
    private val accountDataCleaner: AccountDataCleaner? = null,
    private val pendingCleanupStore: PendingAccountCleanupStore? = null,
) : SessionInvalidator {
    override suspend fun invalidateSession(ownerUserId: String?) {
        var failure: Throwable? = null
        var cleanupSucceeded = ownerUserId == null
        if (ownerUserId != null) {
            runCatching { pendingCleanupStore?.add(ownerUserId) }
                .onFailure { failure = it }
            cleanupSucceeded =
                runCatching { accountDataCleaner?.clear(ownerUserId) }
                    .onFailure { if (failure == null) failure = it }
                    .isSuccess
        }
        runCatching(tokenVault::clear).onFailure { if (failure == null) failure = it }
        runCatching(refreshAttemptStore::clear)
        stateController.onSessionCleared()
        if (ownerUserId != null && cleanupSucceeded) {
            runCatching { pendingCleanupStore?.remove(ownerUserId) }
        }
        failure?.let { throw it }
    }
}

class InMemoryRefreshAttemptStore(private var key: String = "refresh-attempt-key") : RefreshAttemptStore {
    var clearCount: Int = 0
        private set

    override fun idempotencyKeyFor(refreshToken: RefreshToken): String = key

    override fun clear() {
        key = ""
        clearCount += 1
    }
}

class InMemoryPendingAccountCleanupStore(initialOwners: Set<String> = emptySet()) : PendingAccountCleanupStore {
    private val pendingOwners = initialOwners.toMutableSet()

    override fun owners(): Set<String> = pendingOwners.toSet()

    override fun add(ownerUserId: String) {
        pendingOwners += ownerUserId
    }

    override fun remove(ownerUserId: String) {
        pendingOwners -= ownerUserId
    }
}
