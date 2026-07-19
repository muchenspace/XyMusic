package com.xymusic.app.feature.auth.data

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.database.AccountDataCleaner
import com.xymusic.app.core.database.PendingAccountCleanupStore
import com.xymusic.app.core.database.dao.AccountDataDeletion
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.network.NetworkFailureReason
import com.xymusic.app.core.network.ProblemMapper
import com.xymusic.app.core.network.ServerConfigRepository
import com.xymusic.app.core.network.ServerEndpoint
import com.xymusic.app.core.network.ServerProtocol
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.network.auth.RefreshAttemptStore
import com.xymusic.app.core.security.AccessToken
import com.xymusic.app.core.security.RefreshToken
import com.xymusic.app.core.security.SessionTokens
import com.xymusic.app.core.security.TokenVault
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.core.session.SessionInvalidator
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.core.session.SessionStateController
import com.xymusic.app.data.network.BearerTokenInterceptor
import com.xymusic.app.data.network.ProblemResponseParser
import com.xymusic.app.data.network.ServerEndpointInterceptor
import com.xymusic.app.data.network.SessionRequestContext
import com.xymusic.app.data.network.SessionRequestContextBinder
import com.xymusic.app.data.network.SessionRequestContextCallFactory
import com.xymusic.app.data.network.SessionRequestContextInterceptor
import com.xymusic.app.data.network.SessionRequestContextValidationInterceptor
import com.xymusic.app.data.network.auth.model.AuthSessionDto
import com.xymusic.app.data.network.auth.model.AuthSessionInfoDto
import com.xymusic.app.data.network.auth.model.AuthUserDto
import com.xymusic.app.data.network.auth.model.TokenPairDto
import com.xymusic.app.data.network.auth.model.toSessionTokens
import com.xymusic.app.feature.auth.data.remote.LoginRequestDto
import com.xymusic.app.feature.auth.data.remote.PublicAuthApi
import com.xymusic.app.feature.auth.data.remote.RegisterRequestDto
import com.xymusic.app.feature.auth.data.remote.RegistrationResultDto
import com.xymusic.app.feature.auth.data.remote.SessionAuthApi
import com.xymusic.app.feature.auth.domain.AuthResult
import com.xymusic.app.feature.auth.domain.DeviceInfoProvider
import com.xymusic.app.feature.auth.domain.model.DeviceInfo
import com.xymusic.app.feature.auth.domain.model.LoginCommand
import com.xymusic.app.feature.auth.domain.model.RegisterCommand
import com.xymusic.app.support.InMemoryPendingAccountCleanupStore
import com.xymusic.app.support.InMemoryRefreshAttemptStore
import com.xymusic.app.support.InMemoryServerConfigRepository
import com.xymusic.app.support.InMemoryTokenVault
import com.xymusic.app.support.RecordingSessionInvalidator
import com.xymusic.app.support.RecordingSessionStateController
import java.io.IOException
import java.net.ConnectException
import java.net.NoRouteToHostException
import java.net.SocketException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import java.util.concurrent.CancellationException
import javax.net.ssl.SSLHandshakeException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.withTimeout
import kotlinx.serialization.json.Json
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.ResponseBody.Companion.toResponseBody
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Test
import retrofit2.Response
import retrofit2.Retrofit

@OptIn(ExperimentalCoroutinesApi::class)
class DefaultAuthRepositoryTest {
    @Test
    fun registrationSendsUsernameAndPasswordAndMapsActiveState() = runTest {
        val api =
            FakePublicAuthApi().apply {
                registerResponse =
                    Response.success(
                        RegistrationResultDto(
                            userId = "user-1",
                            username = "alice_01",
                            status = "ACTIVE",
                        ),
                    )
            }
        val repository = repository(api = api)

        val result =
            repository.register(
                RegisterCommand(
                    username = "alice_01",
                    password = "password-123",
                ),
            )

        assertThat(api.registerRequest).isEqualTo(
            RegisterRequestDto(
                username = "alice_01",
                password = "password-123",
            ),
        )
        assertThat(result).isInstanceOf(AuthResult.Success::class.java)
        val registration = (result as AuthResult.Success).value
        assertThat(registration.userId).isEqualTo("user-1")
        assertThat(registration.username).isEqualTo("alice_01")
    }

    @Test
    fun loginAttachesStableDeviceAndPublishesStoredSession() = runTest {
        val api =
            FakePublicAuthApi().apply {
                loginResponse = Response.success(authSession())
            }
        val vault = InMemoryTokenVault()
        val refreshStore = InMemoryRefreshAttemptStore()
        val sessionController = RecordingSessionStateController()
        val repository =
            repository(
                api = api,
                vault = vault,
                refreshStore = refreshStore,
                sessionController = sessionController,
            )

        val result = repository.login(LoginCommand("alice_01", "password-123"))

        assertThat(result).isEqualTo(AuthResult.Success(Unit))
        assertThat(api.loginRequest?.username).isEqualTo("alice_01")
        assertThat(api.loginRequest?.device?.installationId)
            .isEqualTo("11111111-1111-4111-8111-111111111111")
        assertThat(api.loginRequest?.device?.platform).isEqualTo("ANDROID")
        assertThat(vault.read()?.userId).isEqualTo(USER_ID)
        assertThat(sessionController.availableTokens?.sessionId).isEqualTo(SESSION_ID)
        assertThat(refreshStore.clearCount).isEqualTo(1)
    }

    @Test
    fun loginClassifiesTransportFailures() = runTest {
        val cases =
            listOf(
                ConnectException("Connection refused") to NetworkFailureReason.ConnectionRefused,
                UnknownHostException("missing.example") to NetworkFailureReason.HostUnresolved,
                SocketTimeoutException("timeout") to NetworkFailureReason.Timeout,
                SSLHandshakeException("certificate rejected") to NetworkFailureReason.SecureConnectionFailed,
                NoRouteToHostException("No route to host") to NetworkFailureReason.NoRoute,
                SocketException("Connection reset") to NetworkFailureReason.ConnectionLost,
                IOException("offline") to NetworkFailureReason.Unknown,
            )

        cases.forEach { (failure, expectedReason) ->
            val api = FakePublicAuthApi().apply { loginFailure = failure }

            val result = repository(api = api).login(LoginCommand("alice_01", "password-123"))

            assertThat(result).isInstanceOf(AuthResult.Failure::class.java)
            val error = (result as AuthResult.Failure).error
            assertThat(error).isInstanceOf(DomainError.Network::class.java)
            assertThat((error as DomainError.Network).reason).isEqualTo(expectedReason)
        }
    }

    @Test
    fun loginDoesNotMisclassifyLocalTokenPersistenceFailureAsNetwork() = runTest {
        val api = FakePublicAuthApi().apply { loginResponse = Response.success(authSession()) }
        val vault =
            object : TokenVault {
                override fun read(): SessionTokens? = null

                override fun write(tokens: SessionTokens): Unit = throw IOException("storage unavailable")

                override fun clear() = Unit
            }

        val result = repository(api = api, vault = vault).login(LoginCommand("alice_01", "password-123"))

        assertThat(result).isInstanceOf(AuthResult.Failure::class.java)
        assertThat((result as AuthResult.Failure).error).isInstanceOf(DomainError.Local::class.java)
    }

    @Test
    fun loginDoesNotMisclassifyDeviceInformationFailureAsNetwork() = runTest {
        val result =
            repository(
                api = FakePublicAuthApi(),
                deviceInfoProvider = DeviceInfoProvider { throw IOException("device data unavailable") },
            ).login(LoginCommand("alice_01", "password-123"))

        assertThat(result).isInstanceOf(AuthResult.Failure::class.java)
        assertThat((result as AuthResult.Failure).error).isInstanceOf(DomainError.Local::class.java)
    }

    @Test
    fun duplicateUsernameProblemRemainsTypedForPresentation() = runTest {
        val api =
            FakePublicAuthApi().apply {
                registerResponse =
                    Response.error(
                        409,
                        """
                            {
                              "type":"https://api.xymusic.example/problems/duplicate-username",
                              "title":"Conflict",
                              "status":409,
                              "code":"DUPLICATE_USERNAME",
                              "detail":"Username is already registered",
                              "traceId":"trace-12345678"
                            }
                        """.trimIndent().toResponseBody(PROBLEM_MEDIA_TYPE),
                    )
            }

        val result =
            repository(api = api).register(
                RegisterCommand("alice_01", "password-123"),
            )

        assertThat(result).isInstanceOf(AuthResult.Failure::class.java)
        val error = (result as AuthResult.Failure).error
        assertThat(error).isInstanceOf(DomainError.Conflict::class.java)
        assertThat((error as DomainError.Conflict).reason.name).isEqualTo("DuplicateUsername")
    }

    @Test
    fun logoutClearsLocalStateEvenWhenRemoteRevocationCannotConnect() = runTest {
        val vault = InMemoryTokenVault(authSession().toTokensForTest())
        val refreshStore = InMemoryRefreshAttemptStore()
        val sessionController = RecordingSessionStateController()
        val cleaner = RecordingAccountDataCleaner()
        val repository =
            repository(
                api = FakePublicAuthApi(),
                sessionApi = SessionAuthApi { _ -> throw IOException("offline") },
                vault = vault,
                refreshStore = refreshStore,
                sessionController = sessionController,
                cleaner = cleaner,
            )

        val result = repository.logout()

        assertThat(result).isEqualTo(AuthResult.Success(Unit))
        assertThat(cleaner.clearedOwner).isEqualTo(USER_ID)
        assertThat(vault.read()).isNull()
        assertThat(refreshStore.clearCount).isEqualTo(1)
        assertThat(sessionController.cleared).isTrue()
    }

    @Test
    fun logoutDoesNotHoldSessionMutexWhileAuthenticatedRequestRuns() = runTest {
        val vault = InMemoryTokenVault(authSession().toTokensForTest())
        val mutationCoordinator = SessionMutationCoordinator()
        val repository =
            repository(
                api = FakePublicAuthApi(),
                sessionApi =
                SessionAuthApi { _ ->
                    mutationCoordinator.mutate { Unit }
                    Response.success(Unit)
                },
                vault = vault,
                mutationCoordinator = mutationCoordinator,
            )

        val result = withTimeout(1_000) { repository.logout() }

        assertThat(result).isEqualTo(AuthResult.Success(Unit))
        assertThat(vault.read()).isNull()
    }

    @Test
    fun logoutWithoutCapturedSessionDoesNotInvokeUnconditionalInvalidation() = runTest {
        val vault = InMemoryTokenVault()
        var invalidationCount = 0
        val invalidator =
            SessionInvalidator {
                invalidationCount += 1
            }
        val repository =
            repository(
                api = FakePublicAuthApi(),
                vault = vault,
                sessionInvalidator = invalidator,
            )

        val result = repository.logout()

        assertThat(result).isEqualTo(AuthResult.Success(Unit))
        assertThat(invalidationCount).isEqualTo(0)
    }

    @Test
    fun logoutDuringServerSwitchSkipsRemoteButStillClearsCapturedSession() = runTest {
        val runtime = ServerRuntimeCoordinator()
        val tokens = authSession().toTokensForTest()
        val identity =
            ActiveSessionIdentity(
                tokens.userId,
                tokens.sessionId,
                runtime.captureGeneration(),
            )
        val switchingGeneration = runtime.beginSwitch()
        val vault = InMemoryTokenVault(tokens)
        var remoteCallCount = 0
        var invalidatedIdentity: ActiveSessionIdentity? = null
        val invalidator =
            object : SessionInvalidator {
                override suspend fun invalidateSession(ownerUserId: String?) = Unit

                override suspend fun invalidateSessionIfCurrent(expectedIdentity: ActiveSessionIdentity) {
                    invalidatedIdentity = expectedIdentity
                    vault.clear()
                }
            }
        val repository =
            repository(
                api = FakePublicAuthApi(),
                sessionApi =
                SessionAuthApi { _ ->
                    remoteCallCount += 1
                    Response.success(Unit)
                },
                vault = vault,
                sessionInvalidator = invalidator,
                sessionIdentityProvider = SessionIdentityProvider { identity },
                serverRuntimeCoordinator = runtime,
            )

        val result =
            try {
                repository.logout()
            } finally {
                runtime.finishSwitch(switchingGeneration)
            }

        assertThat(result).isEqualTo(AuthResult.Success(Unit))
        assertThat(remoteCallCount).isEqualTo(0)
        assertThat(invalidatedIdentity).isEqualTo(identity)
        assertThat(vault.read()).isNull()
    }

    @Test
    fun tokenVaultReadFailureStillTriggersIdentityScopedLocalInvalidation() = runTest {
        val runtime = ServerRuntimeCoordinator()
        val tokens = authSession().toTokensForTest()
        val identity =
            ActiveSessionIdentity(
                tokens.userId,
                tokens.sessionId,
                runtime.captureGeneration(),
            )
        var clearCount = 0
        val failingVault =
            object : TokenVault {
                override fun read(): SessionTokens? = throw IOException("storage unavailable")

                override fun write(tokens: SessionTokens) = Unit

                override fun clear() {
                    clearCount += 1
                }
            }
        var remoteCallCount = 0
        var invalidatedIdentity: ActiveSessionIdentity? = null
        val invalidator =
            object : SessionInvalidator {
                override suspend fun invalidateSession(ownerUserId: String?) = Unit

                override suspend fun invalidateSessionIfCurrent(expectedIdentity: ActiveSessionIdentity) {
                    invalidatedIdentity = expectedIdentity
                    failingVault.clear()
                }
            }
        val repository =
            repository(
                api = FakePublicAuthApi(),
                sessionApi =
                SessionAuthApi { _ ->
                    remoteCallCount += 1
                    Response.success(Unit)
                },
                vault = failingVault,
                sessionInvalidator = invalidator,
                sessionIdentityProvider = SessionIdentityProvider { identity },
                serverRuntimeCoordinator = runtime,
            )

        val result = repository.logout()

        assertThat(result).isEqualTo(AuthResult.Success(Unit))
        assertThat(remoteCallCount).isEqualTo(0)
        assertThat(invalidatedIdentity).isEqualTo(identity)
        assertThat(clearCount).isEqualTo(1)
    }

    @Test
    fun delayedLogoutCleansCapturedOwnerWithoutClearingReplacementSession() = runTest {
        val oldTokens = authSession().toTokensForTest()
        val newTokens =
            oldTokens.copy(
                userId = "33333333-3333-4333-8333-333333333333",
                sessionId = "44444444-4444-4444-8444-444444444444",
                accessToken = AccessToken.from("replacement-access-token-123456789012345"),
                refreshToken = RefreshToken.from("replacement-refresh-token-12345678901234"),
            )
        val runtime = ServerRuntimeCoordinator()
        val oldIdentity =
            ActiveSessionIdentity(
                oldTokens.userId,
                oldTokens.sessionId,
                runtime.captureGeneration(),
            )
        val newIdentity =
            ActiveSessionIdentity(
                newTokens.userId,
                newTokens.sessionId,
                runtime.captureGeneration(),
            )
        var activeIdentity: ActiveSessionIdentity? = oldIdentity
        val vault = InMemoryTokenVault(oldTokens)
        val sessionController =
            RecordingSessionStateController().apply {
                onSessionAvailable(oldTokens)
            }
        val cleaner = RecordingAccountDataCleaner()
        var conditionalIdentity: ActiveSessionIdentity? = null
        var remoteContext: SessionRequestContext? = null
        var unconditionalInvalidationCount = 0
        val invalidator =
            object : SessionInvalidator {
                override suspend fun invalidateSession(ownerUserId: String?) {
                    unconditionalInvalidationCount += 1
                    vault.clear()
                    sessionController.onSessionCleared()
                }

                override suspend fun invalidateSessionIfCurrent(expectedIdentity: ActiveSessionIdentity) {
                    conditionalIdentity = expectedIdentity
                    cleaner.clear(expectedIdentity.userId)
                    if (activeIdentity == expectedIdentity) {
                        vault.clear()
                        sessionController.onSessionCleared()
                    }
                }
            }
        val remoteStarted = CompletableDeferred<Unit>()
        val allowRemoteToFinish = CompletableDeferred<Unit>()
        val repository =
            repository(
                api = FakePublicAuthApi(),
                sessionApi =
                SessionAuthApi { requestContext ->
                    remoteContext = requestContext
                    remoteStarted.complete(Unit)
                    allowRemoteToFinish.await()
                    Response.success(Unit)
                },
                vault = vault,
                sessionController = sessionController,
                cleaner = cleaner,
                sessionInvalidator = invalidator,
                sessionIdentityProvider = SessionIdentityProvider { activeIdentity },
                serverRuntimeCoordinator = runtime,
            )

        val logout = async { repository.logout() }
        remoteStarted.await()
        activeIdentity = newIdentity
        vault.write(newTokens)
        sessionController.onSessionAvailable(newTokens)
        allowRemoteToFinish.complete(Unit)

        assertThat(logout.await()).isEqualTo(AuthResult.Success(Unit))
        assertThat(conditionalIdentity).isEqualTo(oldIdentity)
        assertThat(remoteContext?.identityOrNull()).isEqualTo(oldIdentity)
        assertThat(remoteContext?.serverEndpoint).isEqualTo(TEST_ENDPOINT)
        assertThat(remoteContext?.accessToken).isEqualTo(oldTokens.accessToken)
        assertThat(remoteContext?.accessToken).isNotEqualTo(newTokens.accessToken)
        assertThat(unconditionalInvalidationCount).isEqualTo(0)
        assertThat(cleaner.clearedOwner).isEqualTo(oldTokens.userId)
        assertThat(vault.read()?.sessionId).isEqualTo(newTokens.sessionId)
        assertThat(sessionController.availableTokens?.sessionId).isEqualTo(newTokens.sessionId)
    }

    @Test
    fun delayedLogoutRequestIsRejectedInsteadOfUsingReplacementSessionToken() = runTest {
        val server = MockWebServer()
        server.enqueue(MockResponse().setResponseCode(204))
        server.start()
        try {
            val serverConfig = InMemoryServerConfigRepository.from(server.url("/"))
            val runtime = ServerRuntimeCoordinator()
            val oldTokens = authSession().toTokensForTest()
            val newTokens =
                oldTokens.copy(
                    userId = "33333333-3333-4333-8333-333333333333",
                    sessionId = "44444444-4444-4444-8444-444444444444",
                    accessToken = AccessToken.from("replacement-access-token-123456789012345"),
                    refreshToken = RefreshToken.from("replacement-refresh-token-12345678901234"),
                )
            val oldIdentity =
                ActiveSessionIdentity(
                    oldTokens.userId,
                    oldTokens.sessionId,
                    runtime.captureGeneration(),
                )
            val newIdentity =
                ActiveSessionIdentity(
                    newTokens.userId,
                    newTokens.sessionId,
                    runtime.captureGeneration(),
                )
            var activeIdentity: ActiveSessionIdentity? = oldIdentity
            val identityProvider = SessionIdentityProvider { activeIdentity }
            val vault = InMemoryTokenVault(oldTokens)
            val binder =
                SessionRequestContextBinder(
                    tokenVault = vault,
                    sessionIdentityProvider = identityProvider,
                    serverRuntimeCoordinator = runtime,
                    serverConfigRepository = serverConfig,
                )
            val client =
                OkHttpClient
                    .Builder()
                    .addInterceptor(SessionRequestContextInterceptor(binder))
                    .addInterceptor(ServerEndpointInterceptor(serverConfig, runtime))
                    .addInterceptor(
                        BearerTokenInterceptor(
                            tokenVault = vault,
                            serverConfigRepository = serverConfig,
                            serverRuntimeCoordinator = runtime,
                            sessionIdentityProvider = identityProvider,
                        ),
                    ).addNetworkInterceptor(
                        SessionRequestContextValidationInterceptor(
                            tokenVault = vault,
                            sessionIdentityProvider = identityProvider,
                            serverRuntimeCoordinator = runtime,
                        ),
                    ).build()
            val delegate =
                Retrofit
                    .Builder()
                    .baseUrl(server.url("/"))
                    .callFactory(SessionRequestContextCallFactory(client, binder))
                    .build()
                    .create(SessionAuthApi::class.java)
            val remoteStarted = CompletableDeferred<Unit>()
            val allowRemoteToProceed = CompletableDeferred<Unit>()
            val pausingApi =
                SessionAuthApi { requestContext ->
                    remoteStarted.complete(Unit)
                    allowRemoteToProceed.await()
                    delegate.logout(requestContext)
                }
            val invalidator =
                object : SessionInvalidator {
                    override suspend fun invalidateSession(ownerUserId: String?) = Unit

                    override suspend fun invalidateSessionIfCurrent(expectedIdentity: ActiveSessionIdentity) {
                        if (activeIdentity == expectedIdentity) vault.clear()
                    }
                }
            val repository =
                repository(
                    api = FakePublicAuthApi(),
                    sessionApi = pausingApi,
                    vault = vault,
                    sessionInvalidator = invalidator,
                    sessionIdentityProvider = identityProvider,
                    serverConfigRepository = serverConfig,
                    serverRuntimeCoordinator = runtime,
                )

            val logout = async { repository.logout() }
            remoteStarted.await()
            activeIdentity = newIdentity
            vault.write(newTokens)
            allowRemoteToProceed.complete(Unit)

            assertThat(logout.await()).isEqualTo(AuthResult.Success(Unit))
            assertThat(server.requestCount).isEqualTo(0)
            assertThat(vault.read()?.accessToken).isEqualTo(newTokens.accessToken)
        } finally {
            server.shutdown()
        }
    }

    @Test
    fun failedRoomCleanupIsPersistedForStartupRetry() = runTest {
        val vault = InMemoryTokenVault(authSession().toTokensForTest())
        val pendingStore = InMemoryPendingAccountCleanupStore()
        val repository =
            repository(
                api = FakePublicAuthApi(),
                vault = vault,
                pendingCleanupStore = pendingStore,
                cleaner =
                object : AccountDataCleaner {
                    override suspend fun clear(ownerUserId: String): AccountDataDeletion =
                        throw IOException("database unavailable")
                },
            )

        val result = repository.logout()

        assertThat(result).isInstanceOf(AuthResult.Failure::class.java)
        assertThat(pendingStore.owners()).containsExactly(USER_ID)
        assertThat(vault.read()).isNull()
    }

    @Test
    fun loginDoesNotPublishSessionWhenPendingCleanupStillFails() = runTest {
        val api = FakePublicAuthApi().apply { loginResponse = Response.success(authSession()) }
        val vault = InMemoryTokenVault()
        val sessionController = RecordingSessionStateController()
        val pendingStore = InMemoryPendingAccountCleanupStore(setOf(USER_ID))
        val repository =
            repository(
                api = api,
                vault = vault,
                sessionController = sessionController,
                pendingCleanupStore = pendingStore,
                cleaner =
                object : AccountDataCleaner {
                    override suspend fun clear(ownerUserId: String): AccountDataDeletion =
                        throw IOException("database unavailable")
                },
            )

        val result = repository.login(LoginCommand("alice_01", "password-123"))

        assertThat(result).isInstanceOf(AuthResult.Failure::class.java)
        assertThat(vault.read()).isNull()
        assertThat(sessionController.availableTokens).isNull()
        assertThat(pendingStore.owners()).containsExactly(USER_ID)
    }

    @Test
    fun loginPropagatesCancellationDuringPendingAccountCleanup() = runTest {
        val api = FakePublicAuthApi().apply { loginResponse = Response.success(authSession()) }
        val vault = InMemoryTokenVault()
        val sessionController = RecordingSessionStateController()
        val pendingStore = InMemoryPendingAccountCleanupStore(setOf(USER_ID))
        val repository =
            repository(
                api = api,
                vault = vault,
                sessionController = sessionController,
                pendingCleanupStore = pendingStore,
                cleaner =
                object : AccountDataCleaner {
                    override suspend fun clear(ownerUserId: String): AccountDataDeletion =
                        throw CancellationException("login cancelled")
                },
            )

        val failure =
            runCatching {
                repository.login(LoginCommand("alice_01", "password-123"))
            }.exceptionOrNull()

        assertThat(failure).isInstanceOf(CancellationException::class.java)
        assertThat(vault.read()).isNull()
        assertThat(sessionController.availableTokens).isNull()
        assertThat(pendingStore.owners()).containsExactly(USER_ID)
    }

    @Test
    fun loginSucceedsWhenStaleRefreshAttemptCannotBeCleared() = runTest {
        val api = FakePublicAuthApi().apply { loginResponse = Response.success(authSession()) }
        val vault = InMemoryTokenVault()
        val sessionController = RecordingSessionStateController()
        val repository =
            repository(
                api = api,
                vault = vault,
                sessionController = sessionController,
                refreshStore =
                object : RefreshAttemptStore {
                    override fun idempotencyKeyFor(refreshToken: RefreshToken): String = "unused"

                    override fun clear(): Unit = throw IOException("preferences unavailable")
                },
            )

        val result = repository.login(LoginCommand("alice_01", "password-123"))

        assertThat(result).isEqualTo(AuthResult.Success(Unit))
        assertThat(vault.read()?.userId).isEqualTo(USER_ID)
        assertThat(sessionController.availableTokens?.userId).isEqualTo(USER_ID)
    }

    private fun repository(
        api: PublicAuthApi,
        sessionApi: SessionAuthApi = SessionAuthApi { _ -> Response.success(Unit) },
        vault: TokenVault = InMemoryTokenVault(),
        refreshStore: RefreshAttemptStore = InMemoryRefreshAttemptStore(),
        sessionController: SessionStateController = RecordingSessionStateController(),
        cleaner: AccountDataCleaner = RecordingAccountDataCleaner(),
        pendingCleanupStore: PendingAccountCleanupStore = InMemoryPendingAccountCleanupStore(),
        mutationCoordinator: SessionMutationCoordinator = SessionMutationCoordinator(),
        sessionInvalidator: SessionInvalidator? = null,
        sessionIdentityProvider: SessionIdentityProvider? = null,
        serverConfigRepository: ServerConfigRepository = InMemoryServerConfigRepository(TEST_ENDPOINT),
        serverRuntimeCoordinator: ServerRuntimeCoordinator = ServerRuntimeCoordinator(),
        deviceInfoProvider: DeviceInfoProvider =
            DeviceInfoProvider {
                DeviceInfo(
                    installationId = "11111111-1111-4111-8111-111111111111",
                    name = "Test device",
                    appVersion = "0.1.0-test",
                )
            },
    ): DefaultAuthRepository {
        val json = Json { ignoreUnknownKeys = true }
        val identityProvider =
            sessionIdentityProvider ?: SessionIdentityProvider {
                vault.read()?.let { tokens ->
                    ActiveSessionIdentity(
                        tokens.userId,
                        tokens.sessionId,
                        serverRuntimeCoordinator.captureGeneration(),
                    )
                }
            }
        return DefaultAuthRepository(
            publicAuthApi = api,
            sessionAuthApi = sessionApi,
            deviceInfoProvider = deviceInfoProvider,
            tokenVault = vault,
            refreshAttemptStore = refreshStore,
            sessionStateController = sessionController,
            sessionInvalidator =
            sessionInvalidator ?: RecordingSessionInvalidator(
                tokenVault = vault,
                refreshAttemptStore = refreshStore,
                stateController = sessionController,
                accountDataCleaner = cleaner,
                pendingCleanupStore = pendingCleanupStore,
            ),
            accountDataCleaner = cleaner,
            pendingAccountCleanupStore = pendingCleanupStore,
            problemResponseParser = ProblemResponseParser(json, ProblemMapper()),
            clock = CLOCK,
            sessionMutationCoordinator = mutationCoordinator,
            ioDispatcher = UnconfinedTestDispatcher(),
            sessionIdentityProvider = identityProvider,
            serverConfigRepository = serverConfigRepository,
            serverRuntimeCoordinator = serverRuntimeCoordinator,
        )
    }

    private fun authSession(): AuthSessionDto = AuthSessionDto(
        user =
        AuthUserDto(
            id = USER_ID,
            username = "alice_01",
            displayName = "Alice",
            bio = null,
            avatar = null,
            role = "USER",
            status = "ACTIVE",
            version = 1,
            createdAt = "2026-07-10T00:00:00Z",
            updatedAt = "2026-07-10T00:00:00Z",
        ),
        session =
        AuthSessionInfoDto(
            id = SESSION_ID,
            deviceName = "Test device",
            createdAt = "2026-07-11T00:00:00Z",
        ),
        tokens =
        TokenPairDto(
            tokenType = "Bearer",
            accessToken = ACCESS_TOKEN,
            accessTokenExpiresAt = "2026-07-11T01:00:00Z",
            refreshToken = REFRESH_TOKEN,
            refreshTokenExpiresAt = "2026-08-11T00:00:00Z",
        ),
    )

    private fun AuthSessionDto.toTokensForTest() = toSessionTokens(CLOCK.millis())

    private class RecordingAccountDataCleaner : AccountDataCleaner {
        var clearedOwner: String? = null

        override suspend fun clear(ownerUserId: String): AccountDataDeletion {
            clearedOwner = ownerUserId
            return AccountDataDeletion(0, 0, 0, 0, 0, 0, 0)
        }
    }

    private class FakePublicAuthApi : PublicAuthApi {
        var registerRequest: RegisterRequestDto? = null
        var loginRequest: LoginRequestDto? = null
        var registerResponse: Response<RegistrationResultDto>? = null
        var loginResponse: Response<AuthSessionDto>? = null
        var loginFailure: IOException? = null

        override suspend fun register(request: RegisterRequestDto): Response<RegistrationResultDto> {
            registerRequest = request
            return checkNotNull(registerResponse)
        }

        override suspend fun login(request: LoginRequestDto): Response<AuthSessionDto> {
            loginRequest = request
            loginFailure?.let { throw it }
            return checkNotNull(loginResponse)
        }
    }

    private companion object {
        val PROBLEM_MEDIA_TYPE = "application/problem+json".toMediaType()
        val CLOCK: Clock = Clock.fixed(Instant.parse("2026-07-11T00:00:00Z"), ZoneOffset.UTC)
        const val USER_ID = "11111111-1111-4111-8111-111111111111"
        const val SESSION_ID = "22222222-2222-4222-8222-222222222222"
        const val ACCESS_TOKEN = "access-token-123456789012345678901234"
        const val REFRESH_TOKEN = "refresh-token-12345678901234567890123"
        val TEST_ENDPOINT: ServerEndpoint =
            checkNotNull(
                ServerEndpoint.parse("api.example.test", "443", ServerProtocol.HTTPS),
            )
    }
}
