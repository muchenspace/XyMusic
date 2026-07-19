package com.xymusic.app.app.session

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.database.AccountDataCleaner
import com.xymusic.app.core.database.PendingAccountCleanupStore
import com.xymusic.app.core.database.dao.AccountDataDeletion
import com.xymusic.app.core.security.AccessToken
import com.xymusic.app.core.security.RefreshToken
import com.xymusic.app.core.security.SessionTokens
import com.xymusic.app.core.security.TokenVault
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.core.sync.PendingSyncScheduler
import com.xymusic.app.support.InMemoryPendingAccountCleanupStore
import com.xymusic.app.support.InMemoryRefreshAttemptStore
import com.xymusic.app.support.InMemoryTokenVault
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import java.util.concurrent.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class DefaultAppSessionProviderTest {
    private val clock = Clock.fixed(Instant.parse("2026-07-10T00:00:00Z"), ZoneOffset.UTC)

    @Test
    fun restoreSessionDefaultsToSignedOutWhenVaultIsEmpty() = runTest {
        val provider =
            DefaultAppSessionProvider(
                InMemoryTokenVault(),
                clock,
                NoOpAccountDataCleaner,
                InMemoryPendingAccountCleanupStore(),
                InMemoryRefreshAttemptStore(),
                NoOpPendingSyncScheduler,
                UnconfinedTestDispatcher(testScheduler),
            )

        assertThat(provider.sessionState.value).isEqualTo(AppSessionState.Loading)
        provider.restoreSession()

        assertThat(provider.sessionState.value).isEqualTo(AppSessionState.SignedOut)
    }

    @Test
    fun restoreSessionUsesPersistedUserWhenRefreshTokenIsValid() = runTest {
        val vault =
            InMemoryTokenVault(
                tokens(refreshExpiresAt = Instant.parse("2026-07-11T00:00:00Z")),
            )
        val provider =
            DefaultAppSessionProvider(
                vault,
                clock,
                NoOpAccountDataCleaner,
                InMemoryPendingAccountCleanupStore(),
                InMemoryRefreshAttemptStore(),
                NoOpPendingSyncScheduler,
                UnconfinedTestDispatcher(testScheduler),
            )

        provider.restoreSession()

        assertThat(provider.sessionState.value).isEqualTo(AppSessionState.SignedIn("user-1"))
        assertThat(vault.readCount).isEqualTo(1)
    }

    @Test
    fun concurrentRestoreCallsReadTheVaultOnlyOnce() = runTest {
        val vault =
            InMemoryTokenVault(
                tokens(refreshExpiresAt = Instant.parse("2026-07-11T00:00:00Z")),
            )
        val provider =
            DefaultAppSessionProvider(
                vault,
                clock,
                NoOpAccountDataCleaner,
                InMemoryPendingAccountCleanupStore(),
                InMemoryRefreshAttemptStore(),
                NoOpPendingSyncScheduler,
                UnconfinedTestDispatcher(testScheduler),
            )

        List(8) { async { provider.restoreSession() } }.awaitAll()

        assertThat(provider.sessionState.value).isEqualTo(AppSessionState.SignedIn("user-1"))
        assertThat(vault.readCount).isEqualTo(1)
    }

    @Test
    fun restoreWaitsForNewerSessionMutationAndDoesNotRepublishPersistedSession() = runTest {
        val persistedTokens = tokens(refreshExpiresAt = Instant.parse("2026-07-11T00:00:00Z"))
        val newerTokens =
            tokens(
                refreshExpiresAt = Instant.parse("2026-07-12T00:00:00Z"),
                userId = "user-2",
                sessionId = "session-2",
            )
        val vault = InMemoryTokenVault(persistedTokens)
        val mutationCoordinator = SessionMutationCoordinator()
        val provider =
            DefaultAppSessionProvider(
                tokenVault = vault,
                clock = clock,
                accountDataCleaner = NoOpAccountDataCleaner,
                pendingAccountCleanupStore = InMemoryPendingAccountCleanupStore(),
                refreshAttemptStore = InMemoryRefreshAttemptStore(),
                pendingSyncScheduler = NoOpPendingSyncScheduler,
                ioDispatcher = UnconfinedTestDispatcher(testScheduler),
                sessionMutationCoordinator = mutationCoordinator,
            )
        val mutationStarted = CompletableDeferred<Unit>()
        val allowMutationToFinish = CompletableDeferred<Unit>()
        val newerMutation =
            async {
                mutationCoordinator.mutate {
                    mutationStarted.complete(Unit)
                    allowMutationToFinish.await()
                    vault.write(newerTokens)
                    provider.onSessionAvailable(newerTokens)
                }
            }
        mutationStarted.await()

        val restore = async { provider.restoreSession() }
        runCurrent()
        assertThat(vault.readCount).isEqualTo(0)
        allowMutationToFinish.complete(Unit)

        newerMutation.await()
        restore.await()
        assertThat(provider.sessionState.value).isEqualTo(AppSessionState.SignedIn("user-2"))
        assertThat(vault.read()?.sessionId).isEqualTo("session-2")
    }

    @Test
    fun restoreSessionClearsExpiredRefreshToken() = runTest {
        val vault =
            InMemoryTokenVault(
                tokens(refreshExpiresAt = Instant.parse("2026-07-09T00:00:00Z")),
            )
        val provider =
            DefaultAppSessionProvider(
                vault,
                clock,
                NoOpAccountDataCleaner,
                InMemoryPendingAccountCleanupStore(),
                InMemoryRefreshAttemptStore(),
                NoOpPendingSyncScheduler,
                UnconfinedTestDispatcher(testScheduler),
            )

        provider.restoreSession()

        assertThat(provider.sessionState.value).isEqualTo(AppSessionState.SignedOut)
        assertThat(vault.read()).isNull()
        assertThat(vault.clearCount).isEqualTo(1)
    }

    @Test
    fun restoreSessionRetriesAndRemovesPendingAccountCleanup() = runTest {
        val store = InMemoryPendingAccountCleanupStore(setOf("stale-user"))
        var cleanedOwner: String? = null
        val provider =
            DefaultAppSessionProvider(
                InMemoryTokenVault(),
                clock,
                object : AccountDataCleaner {
                    override suspend fun clear(ownerUserId: String): AccountDataDeletion {
                        cleanedOwner = ownerUserId
                        return emptyDeletion()
                    }
                },
                store,
                InMemoryRefreshAttemptStore(),
                NoOpPendingSyncScheduler,
                UnconfinedTestDispatcher(testScheduler),
            )

        provider.restoreSession()

        assertThat(cleanedOwner).isEqualTo("stale-user")
        assertThat(store.owners()).isEmpty()
    }

    @Test
    fun restoreSessionPropagatesCancellationDuringPendingAccountCleanup() = runTest {
        val store = InMemoryPendingAccountCleanupStore(setOf("stale-user"))
        val provider =
            DefaultAppSessionProvider(
                InMemoryTokenVault(),
                clock,
                object : AccountDataCleaner {
                    override suspend fun clear(ownerUserId: String): AccountDataDeletion =
                        throw CancellationException("restore cancelled")
                },
                store,
                InMemoryRefreshAttemptStore(),
                NoOpPendingSyncScheduler,
                UnconfinedTestDispatcher(testScheduler),
            )

        val failure = runCatching { provider.restoreSession() }.exceptionOrNull()

        assertThat(failure).isInstanceOf(CancellationException::class.java)
        assertThat(provider.sessionState.value).isEqualTo(AppSessionState.Loading)
        assertThat(store.owners()).containsExactly("stale-user")
    }

    @Test
    fun pendingCleanupOwnerCannotBeRestoredFromAnUnclearedToken() = runTest {
        val vault =
            InMemoryTokenVault(
                tokens(refreshExpiresAt = Instant.parse("2026-07-11T00:00:00Z")),
            )
        val refreshStore = InMemoryRefreshAttemptStore()
        val provider =
            DefaultAppSessionProvider(
                vault,
                clock,
                NoOpAccountDataCleaner,
                InMemoryPendingAccountCleanupStore(setOf("user-1")),
                refreshStore,
                NoOpPendingSyncScheduler,
                UnconfinedTestDispatcher(testScheduler),
            )

        provider.restoreSession()

        assertThat(provider.sessionState.value).isEqualTo(AppSessionState.SignedOut)
        assertThat(vault.read()).isNull()
        assertThat(refreshStore.clearCount).isEqualTo(1)
    }

    @Test
    fun pendingCleanupMarkerReadFailureDoesNotRestorePersistedSession() = runTest {
        val vault =
            InMemoryTokenVault(
                tokens(refreshExpiresAt = Instant.parse("2026-07-11T00:00:00Z")),
            )
        val provider =
            DefaultAppSessionProvider(
                vault,
                clock,
                NoOpAccountDataCleaner,
                object : PendingAccountCleanupStore {
                    override fun owners(): Set<String> = error("preferences unavailable")

                    override fun add(ownerUserId: String) = Unit

                    override fun remove(ownerUserId: String) = Unit
                },
                InMemoryRefreshAttemptStore(),
                NoOpPendingSyncScheduler,
                UnconfinedTestDispatcher(testScheduler),
            )

        provider.restoreSession()

        assertThat(provider.sessionState.value).isEqualTo(AppSessionState.SignedOut)
        assertThat(vault.readCount).isEqualTo(0)
        assertThat(vault.clearCount).isEqualTo(0)
    }

    @Test
    fun invalidationStopsTheSessionAndClearsOwnerData() = runTest {
        val vault =
            InMemoryTokenVault(
                tokens(refreshExpiresAt = Instant.parse("2026-07-11T00:00:00Z")),
            )
        val store = InMemoryPendingAccountCleanupStore()
        var cleanedOwner: String? = null
        var stateDuringCleanup: AppSessionState? = null
        lateinit var provider: DefaultAppSessionProvider
        provider =
            DefaultAppSessionProvider(
                vault,
                clock,
                object : AccountDataCleaner {
                    override suspend fun clear(ownerUserId: String): AccountDataDeletion {
                        cleanedOwner = ownerUserId
                        stateDuringCleanup = provider.sessionState.value
                        return emptyDeletion()
                    }
                },
                store,
                InMemoryRefreshAttemptStore(),
                NoOpPendingSyncScheduler,
                UnconfinedTestDispatcher(testScheduler),
            )
        provider.restoreSession()

        provider.invalidateSession("user-1")

        assertThat(cleanedOwner).isEqualTo("user-1")
        assertThat(stateDuringCleanup).isEqualTo(AppSessionState.SignedOut)
        assertThat(vault.read()).isNull()
        assertThat(store.owners()).isEmpty()
        assertThat(provider.sessionState.value).isEqualTo(AppSessionState.SignedOut)
    }

    @Test
    fun staleConditionalInvalidationCleansOldOwnerWithoutClearingNewSession() = runTest {
        val oldTokens = tokens(refreshExpiresAt = Instant.parse("2026-07-11T00:00:00Z"))
        val newTokens =
            tokens(
                refreshExpiresAt = Instant.parse("2026-07-12T00:00:00Z"),
                userId = "user-2",
                sessionId = "session-2",
            )
        val vault = InMemoryTokenVault(oldTokens)
        val cleanedOwners = mutableListOf<String>()
        val mutationCoordinator = SessionMutationCoordinator()
        val provider =
            DefaultAppSessionProvider(
                tokenVault = vault,
                clock = clock,
                accountDataCleaner =
                object : AccountDataCleaner {
                    override suspend fun clear(ownerUserId: String): AccountDataDeletion {
                        cleanedOwners += ownerUserId
                        return emptyDeletion()
                    }
                },
                pendingAccountCleanupStore = InMemoryPendingAccountCleanupStore(),
                refreshAttemptStore = InMemoryRefreshAttemptStore(),
                pendingSyncScheduler = NoOpPendingSyncScheduler,
                ioDispatcher = UnconfinedTestDispatcher(testScheduler),
                sessionMutationCoordinator = mutationCoordinator,
            )
        provider.restoreSession()
        val oldIdentity = checkNotNull(provider.activeIdentity())
        mutationCoordinator.mutate {
            vault.write(newTokens)
            provider.onSessionAvailable(newTokens)
        }

        mutationCoordinator.mutate {
            provider.invalidateSessionIfCurrent(oldIdentity)
        }

        assertThat(cleanedOwners).containsExactly("user-1")
        assertThat(vault.read()?.sessionId).isEqualTo("session-2")
        assertThat(provider.sessionState.value).isEqualTo(AppSessionState.SignedIn("user-2"))
    }

    @Test
    fun failedInvalidationKeepsRetryMarkerButStillSignsOut() = runTest {
        val vault =
            InMemoryTokenVault(
                tokens(refreshExpiresAt = Instant.parse("2026-07-11T00:00:00Z")),
            )
        val store = InMemoryPendingAccountCleanupStore()
        val provider =
            DefaultAppSessionProvider(
                vault,
                clock,
                object : AccountDataCleaner {
                    override suspend fun clear(ownerUserId: String): AccountDataDeletion =
                        throw IllegalStateException("database unavailable")
                },
                store,
                InMemoryRefreshAttemptStore(),
                NoOpPendingSyncScheduler,
                UnconfinedTestDispatcher(testScheduler),
            )
        provider.restoreSession()

        val failure = runCatching { provider.invalidateSession("user-1") }.exceptionOrNull()

        assertThat(failure).isNotNull()
        assertThat(vault.read()).isNull()
        assertThat(store.owners()).containsExactly("user-1")
        assertThat(provider.sessionState.value).isEqualTo(AppSessionState.SignedOut)
    }

    @Test
    fun invalidationPersistsRetryMarkerBeforeClearingCredentials() = runTest {
        val store = InMemoryPendingAccountCleanupStore()
        val vault =
            object : TokenVault {
                private var value: SessionTokens? =
                    tokens(
                        refreshExpiresAt = Instant.parse("2026-07-11T00:00:00Z"),
                    )

                override fun read(): SessionTokens? = value

                override fun write(tokens: SessionTokens) {
                    value = tokens
                }

                override fun clear() {
                    assertThat(store.owners()).contains("user-1")
                    value = null
                }
            }
        val provider =
            DefaultAppSessionProvider(
                vault,
                clock,
                NoOpAccountDataCleaner,
                store,
                InMemoryRefreshAttemptStore(),
                NoOpPendingSyncScheduler,
                UnconfinedTestDispatcher(testScheduler),
            )
        provider.restoreSession()

        provider.invalidateSession("user-1")

        assertThat(store.owners()).isEmpty()
    }

    @Test
    fun invalidationDoesNotDestroySessionWhenRetryMarkerCannotBePersisted() = runTest {
        val vault =
            InMemoryTokenVault(
                tokens(refreshExpiresAt = Instant.parse("2026-07-11T00:00:00Z")),
            )
        val failingStore =
            object : PendingAccountCleanupStore {
                override fun owners(): Set<String> = emptySet()

                override fun add(ownerUserId: String): Unit = throw IllegalStateException("preferences unavailable")

                override fun remove(ownerUserId: String) = Unit
            }
        val provider =
            DefaultAppSessionProvider(
                vault,
                clock,
                NoOpAccountDataCleaner,
                failingStore,
                InMemoryRefreshAttemptStore(),
                NoOpPendingSyncScheduler,
                UnconfinedTestDispatcher(testScheduler),
            )
        provider.restoreSession()

        val failure = runCatching { provider.invalidateSession("user-1") }.exceptionOrNull()

        assertThat(failure).isNotNull()
        assertThat(vault.read()).isNotNull()
        assertThat(provider.sessionState.value).isEqualTo(AppSessionState.SignedIn("user-1"))
    }

    @Test
    fun startupKeepsRetryMarkerWhenPendingOwnersTokenCannotBeCleared() = runTest {
        val persistedTokens = tokens(refreshExpiresAt = Instant.parse("2026-07-11T00:00:00Z"))
        val vault =
            object : TokenVault {
                override fun read(): SessionTokens = persistedTokens

                override fun write(tokens: SessionTokens) = Unit

                override fun clear(): Unit = throw IllegalStateException("storage unavailable")
            }
        val store = InMemoryPendingAccountCleanupStore(setOf("user-1"))
        val provider =
            DefaultAppSessionProvider(
                vault,
                clock,
                NoOpAccountDataCleaner,
                store,
                InMemoryRefreshAttemptStore(),
                NoOpPendingSyncScheduler,
                UnconfinedTestDispatcher(testScheduler),
            )

        provider.restoreSession()

        assertThat(provider.sessionState.value).isEqualTo(AppSessionState.SignedOut)
        assertThat(store.owners()).containsExactly("user-1")
    }

    private fun tokens(
        refreshExpiresAt: Instant,
        userId: String = "user-1",
        sessionId: String = "session-1",
    ): SessionTokens = SessionTokens(
        userId = userId,
        sessionId = sessionId,
        accessToken = AccessToken.from("access-token-$sessionId"),
        accessTokenExpiresAtEpochMillis = Instant.parse("2026-07-10T00:05:00Z").toEpochMilli(),
        refreshToken = RefreshToken.from("refresh-token-$sessionId"),
        refreshTokenExpiresAtEpochMillis = refreshExpiresAt.toEpochMilli(),
    )

    private data object NoOpAccountDataCleaner : AccountDataCleaner {
        override suspend fun clear(ownerUserId: String): AccountDataDeletion = emptyDeletion()
    }

    private data object NoOpPendingSyncScheduler : PendingSyncScheduler {
        override fun schedule(ownerUserId: String) = Unit

        override fun cancel(ownerUserId: String) = Unit
    }

    private companion object {
        fun emptyDeletion() = AccountDataDeletion(0, 0, 0, 0, 0, 0, 0)
    }
}
