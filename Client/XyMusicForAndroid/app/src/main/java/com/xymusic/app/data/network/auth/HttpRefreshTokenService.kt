package com.xymusic.app.data.network.auth

import com.xymusic.app.core.network.ApiBaseUrl
import com.xymusic.app.core.network.ServerConfigRepository
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.network.ServerSwitchInProgressException
import com.xymusic.app.core.network.StaleServerGenerationException
import com.xymusic.app.core.network.auth.RefreshAttemptStore
import com.xymusic.app.core.network.auth.RefreshTokenRequest
import com.xymusic.app.core.network.auth.RefreshTokenService
import com.xymusic.app.core.network.auth.TokenRefreshRejectedException
import com.xymusic.app.core.security.SessionTokens
import com.xymusic.app.data.network.ExpectedServerRequestContext
import com.xymusic.app.data.network.ProblemResponseParser
import com.xymusic.app.data.network.auth.model.AuthSessionDto
import com.xymusic.app.data.network.auth.model.toSessionTokens
import java.io.IOException
import java.time.Clock
import javax.inject.Inject
import kotlinx.coroutines.withTimeout
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response

class HttpRefreshTokenService
@Inject
constructor(
    @ApiBaseUrl private val apiBaseUrl: HttpUrl,
    private val callExecutor: AuthCallExecutor,
    private val refreshAttemptStore: RefreshAttemptStore,
    private val problemResponseParser: ProblemResponseParser,
    private val json: Json,
    private val clock: Clock,
    private val serverConfigRepository: ServerConfigRepository,
    private val serverRuntimeCoordinator: ServerRuntimeCoordinator,
) : RefreshTokenService {
    override suspend fun refresh(request: RefreshTokenRequest): SessionTokens = withTimeout(REFRESH_TOTAL_TIMEOUT_MS) {
        val httpRequest = createRequest(request)
        var lastTransportFailure: IOException? = null
        repeat(MAX_TRANSPORT_ATTEMPTS) {
            try {
                return@withTimeout callExecutor.execute(httpRequest).use(::parseResponse)
            } catch (failure: TokenRefreshRejectedException) {
                throw failure
            } catch (failure: TokenRefreshProtocolException) {
                throw failure
            } catch (failure: StaleServerGenerationException) {
                throw failure
            } catch (failure: ServerSwitchInProgressException) {
                throw failure
            } catch (failure: IOException) {
                lastTransportFailure = failure
            }
        }
        throw checkNotNull(lastTransportFailure)
    }

    private fun createRequest(refreshRequest: RefreshTokenRequest): Request {
        val generation = refreshRequest.expectedIdentity.serverGeneration
        serverRuntimeCoordinator.requireCurrent(generation)
        val serverEndpoint =
            serverConfigRepository.currentEndpoint()
                ?: throw IOException("Server endpoint is not configured")
        serverRuntimeCoordinator.requireCurrent(generation)
        val endpoint =
            checkNotNull(apiBaseUrl.resolve(REFRESH_PATH)) {
                "API base URL cannot resolve refresh endpoint"
            }
        val requestJson =
            json.encodeToString(
                RefreshRequestDto(refreshRequest.refreshToken.value),
            )
        return Request
            .Builder()
            .url(endpoint)
            .header(
                IDEMPOTENCY_KEY_HEADER,
                refreshAttemptStore.idempotencyKeyFor(refreshRequest.refreshToken),
            ).tag(
                ExpectedServerRequestContext::class.java,
                ExpectedServerRequestContext(serverEndpoint, generation),
            ).post(requestJson.toRequestBody(JSON_MEDIA_TYPE))
            .build()
    }

    private fun parseResponse(response: Response): SessionTokens {
        val body = response.body.string()
        if (!response.isSuccessful) {
            val error =
                problemResponseParser.parse(
                    status = response.code,
                    body = body,
                    traceId = response.header(TRACE_ID_HEADER),
                    retryAfterSeconds = response.header(RETRY_AFTER_HEADER)?.toLongOrNull(),
                )
            throw TokenRefreshRejectedException(error)
        }

        return decodeSession(body).toValidatedSessionTokens()
    }

    private fun decodeSession(body: String): AuthSessionDto = try {
        json.decodeFromString<AuthSessionDto>(body)
    } catch (failure: SerializationException) {
        throw TokenRefreshProtocolException("Malformed refresh response", failure)
    } catch (failure: IllegalArgumentException) {
        throw TokenRefreshProtocolException("Malformed refresh response", failure)
    }

    private fun AuthSessionDto.toValidatedSessionTokens(): SessionTokens = try {
        toSessionTokens(clock.millis())
    } catch (failure: RuntimeException) {
        throw TokenRefreshProtocolException("Invalid refresh token payload", failure)
    }

    private companion object {
        const val REFRESH_PATH = "api/v1/auth/refresh"
        const val IDEMPOTENCY_KEY_HEADER = "Idempotency-Key"
        const val TRACE_ID_HEADER = "X-Trace-Id"
        const val RETRY_AFTER_HEADER = "Retry-After"
        const val MAX_TRANSPORT_ATTEMPTS = 2
        const val REFRESH_TOTAL_TIMEOUT_MS = 30_000L
        val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
    }
}

@Serializable
private data class RefreshRequestDto(val refreshToken: String)

private class TokenRefreshProtocolException(message: String, cause: Throwable) : IOException(message, cause)
