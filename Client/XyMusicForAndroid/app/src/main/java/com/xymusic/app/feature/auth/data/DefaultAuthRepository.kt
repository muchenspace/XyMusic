package com.xymusic.app.feature.auth.data

import com.xymusic.app.core.common.IoDispatcher
import com.xymusic.app.core.common.runCatchingPreservingCancellation
import com.xymusic.app.core.database.AccountDataCleaner
import com.xymusic.app.core.database.PendingAccountCleanupStore
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.network.ServerConfigRepository
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.network.auth.RefreshAttemptStore
import com.xymusic.app.core.network.toDomainNetworkError
import com.xymusic.app.core.security.SessionTokens
import com.xymusic.app.core.security.TokenVault
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.core.session.SessionInvalidator
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.core.session.SessionStateController
import com.xymusic.app.data.network.ProblemResponseParser
import com.xymusic.app.data.network.SessionRequestContext
import com.xymusic.app.data.network.auth.model.toSessionTokens
import com.xymusic.app.feature.auth.data.remote.ACTIVE_STATUS
import com.xymusic.app.feature.auth.data.remote.LoginRequestDto
import com.xymusic.app.feature.auth.data.remote.PublicAuthApi
import com.xymusic.app.feature.auth.data.remote.RegisterRequestDto
import com.xymusic.app.feature.auth.data.remote.RegistrationResultDto
import com.xymusic.app.feature.auth.data.remote.SessionAuthApi
import com.xymusic.app.feature.auth.data.remote.toDto
import com.xymusic.app.feature.auth.domain.AuthRepository
import com.xymusic.app.feature.auth.domain.AuthResult
import com.xymusic.app.feature.auth.domain.DeviceInfoProvider
import com.xymusic.app.feature.auth.domain.model.LoginCommand
import com.xymusic.app.feature.auth.domain.model.RegisterCommand
import com.xymusic.app.feature.auth.domain.model.RegistrationResult
import java.io.IOException
import java.time.Clock
import java.util.concurrent.CancellationException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.withContext
import retrofit2.Response

@Singleton
class DefaultAuthRepository
@Inject
constructor(
    private val publicAuthApi: PublicAuthApi,
    private val sessionAuthApi: SessionAuthApi,
    private val deviceInfoProvider: DeviceInfoProvider,
    private val tokenVault: TokenVault,
    private val refreshAttemptStore: RefreshAttemptStore,
    private val sessionStateController: SessionStateController,
    private val sessionInvalidator: SessionInvalidator,
    private val accountDataCleaner: AccountDataCleaner,
    private val pendingAccountCleanupStore: PendingAccountCleanupStore,
    private val problemResponseParser: ProblemResponseParser,
    private val clock: Clock,
    private val sessionMutationCoordinator: SessionMutationCoordinator,
    @IoDispatcher private val ioDispatcher: CoroutineDispatcher,
    private val serverConfigRepository: ServerConfigRepository,
    private val serverRuntimeCoordinator: ServerRuntimeCoordinator,
    private val sessionIdentityProvider: SessionIdentityProvider = SessionIdentityProvider { null },
) : AuthRepository {
    override suspend fun register(command: RegisterCommand): AuthResult<RegistrationResult> = executeWithBody(
        request = {
            publicAuthApi.register(
                RegisterRequestDto(
                    username = command.username,
                    password = command.password,
                ),
            )
        },
        map = { it.toDomain() },
    )

    override suspend fun login(command: LoginCommand): AuthResult<Unit> = withContext(ioDispatcher) {
        sessionMutationCoordinator.mutate {
            val request =
                runCatchingPreservingCancellation {
                    LoginRequestDto(
                        username = command.username,
                        password = command.password,
                        device = deviceInfoProvider.get().toDto(),
                    )
                }.getOrElse { return@mutate localLoginPreparationFailure() }
            val remoteResult =
                executeApiCall {
                    val response = publicAuthApi.login(request)
                    if (!response.isSuccessful) return@executeApiCall failure(response)
                    val session = response.body() ?: return@executeApiCall protocolFailure()
                    val tokens =
                        runCatching { session.toSessionTokens(clock.millis()) }
                            .getOrElse { return@executeApiCall protocolFailure() }
                    AuthResult.Success(tokens)
                }
            when (remoteResult) {
                is AuthResult.Failure -> remoteResult
                is AuthResult.Success -> {
                    val tokens = remoteResult.value
                    clearPendingDataBeforeLogin(tokens.userId)?.let { failure ->
                        return@mutate failure
                    }
                    if (runCatchingPreservingCancellation { tokenVault.write(tokens) }.isFailure) {
                        return@mutate localSessionFailure()
                    }
                    runCatching(refreshAttemptStore::clear)
                    sessionStateController.onSessionAvailable(tokens)
                    AuthResult.Success(Unit)
                }
            }
        }
    }

    override suspend fun logout(): AuthResult<Unit> = withContext(ioDispatcher) {
        val snapshot =
            sessionMutationCoordinator.mutate {
                val identity = runCatching(sessionIdentityProvider::activeIdentity).getOrNull()
                val tokens = runCatching(tokenVault::read).getOrNull()
                LogoutSessionSnapshot(
                    ownerUserId = tokens?.userId ?: identity?.userId,
                    identity = identity,
                    requestContext = buildLogoutRequestContext(tokens, identity),
                )
            }
        var localResult: AuthResult<Unit> = AuthResult.Success(Unit)
        try {
            if (snapshot.requestContext != null) {
                executeApiCall {
                    val response = sessionAuthApi.logout(snapshot.requestContext)
                    if (response.isSuccessful) AuthResult.Success(Unit) else failure(response)
                }
            }
        } finally {
            withContext(NonCancellable) {
                sessionMutationCoordinator.mutate {
                    localResult = clearLocalSession(snapshot.ownerUserId, snapshot.identity)
                }
            }
        }
        localResult
    }

    private suspend fun clearLocalSession(
        ownerUserId: String?,
        expectedIdentity: ActiveSessionIdentity?,
    ): AuthResult<Unit> {
        if (ownerUserId == null && expectedIdentity == null) return AuthResult.Success(Unit)
        val localFailure =
            runCatching {
                if (expectedIdentity == null) {
                    sessionInvalidator.invalidateSession(ownerUserId)
                } else {
                    sessionInvalidator.invalidateSessionIfCurrent(expectedIdentity)
                }
            }.exceptionOrNull()
        return if (localFailure == null) {
            AuthResult.Success(Unit)
        } else {
            AuthResult.Failure(
                DomainError.Local(
                    detail = "Unable to clear local account data",
                ),
            )
        }
    }

    private suspend fun clearPendingDataBeforeLogin(ownerUserId: String): AuthResult.Failure? {
        val pendingOwners =
            runCatching(pendingAccountCleanupStore::owners)
                .getOrElse { return localDataFailure() }
        if (ownerUserId !in pendingOwners) return null

        return runCatchingPreservingCancellation {
            accountDataCleaner.clear(ownerUserId)
            pendingAccountCleanupStore.remove(ownerUserId)
        }.fold(
            onSuccess = { null },
            onFailure = { localDataFailure() },
        )
    }

    private suspend fun <T, R> executeWithBody(request: suspend () -> Response<T>, map: (T) -> R): AuthResult<R> =
        withContext(ioDispatcher) {
            executeApiCall {
                val response = request()
                if (!response.isSuccessful) return@executeApiCall failure(response)
                val body = response.body() ?: return@executeApiCall protocolFailure()
                val mapped =
                    runCatching { map(body) }
                        .getOrElse { return@executeApiCall protocolFailure() }
                AuthResult.Success(mapped)
            }
        }

    private suspend fun <T> executeApiCall(block: suspend () -> AuthResult<T>): AuthResult<T> = try {
        block()
    } catch (failure: CancellationException) {
        throw failure
    } catch (failure: IOException) {
        AuthResult.Failure(failure.toDomainNetworkError())
    } catch (_: Exception) {
        protocolFailure()
    }

    private fun failure(response: Response<*>): AuthResult.Failure = AuthResult.Failure(
        problemResponseParser.parse(
            status = response.code(),
            body = response.errorBody()?.string(),
            traceId = response.headers()[TRACE_ID_HEADER],
            retryAfterSeconds = response.headers()[RETRY_AFTER_HEADER]?.toLongOrNull(),
        ),
    )

    private fun protocolFailure(): AuthResult.Failure = AuthResult.Failure(
        DomainError.Protocol(
            detail = "Invalid authentication response",
            traceId = null,
            status = null,
        ),
    )

    private fun localDataFailure(): AuthResult.Failure = AuthResult.Failure(
        DomainError.Local(
            detail = "Unable to clear previous account data",
        ),
    )

    private fun localSessionFailure(): AuthResult.Failure = AuthResult.Failure(
        DomainError.Local(
            detail = "Unable to store the local authentication session",
        ),
    )

    private fun localLoginPreparationFailure(): AuthResult.Failure = AuthResult.Failure(
        DomainError.Local(
            detail = "Unable to read local device information",
        ),
    )

    private fun SessionTokens.belongsTo(identity: ActiveSessionIdentity): Boolean =
        userId == identity.userId && sessionId == identity.sessionId

    private fun buildLogoutRequestContext(
        tokens: SessionTokens?,
        identity: ActiveSessionIdentity?,
    ): SessionRequestContext? {
        if (tokens == null || identity == null || !tokens.belongsTo(identity)) return null
        return try {
            serverRuntimeCoordinator.requireCurrent(identity.serverGeneration)
            val endpoint = serverConfigRepository.currentEndpoint() ?: return null
            serverRuntimeCoordinator.requireCurrent(identity.serverGeneration)
            SessionRequestContext(
                userId = identity.userId,
                sessionId = identity.sessionId,
                serverGeneration = identity.serverGeneration,
                serverEndpoint = endpoint,
                accessToken = tokens.accessToken,
            )
        } catch (_: Exception) {
            null
        }
    }

    private data class LogoutSessionSnapshot(
        val ownerUserId: String?,
        val identity: ActiveSessionIdentity?,
        val requestContext: SessionRequestContext?,
    )

    private fun RegistrationResultDto.toDomain(): RegistrationResult {
        require(status == ACTIVE_STATUS) { "Unexpected registration status" }
        return RegistrationResult(
            userId = userId,
            username = username,
        )
    }

    private companion object {
        const val TRACE_ID_HEADER = "X-Trace-Id"
        const val RETRY_AFTER_HEADER = "Retry-After"
    }
}
