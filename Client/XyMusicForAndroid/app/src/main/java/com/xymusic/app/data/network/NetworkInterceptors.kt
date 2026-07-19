package com.xymusic.app.data.network

import com.xymusic.app.BuildConfig
import com.xymusic.app.core.network.ServerConfigRepository
import com.xymusic.app.core.network.ServerEndpoint
import com.xymusic.app.core.network.ServerGeneration
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.network.StaleServerGenerationException
import com.xymusic.app.core.security.AccessToken
import com.xymusic.app.core.security.TokenVault
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.core.session.SessionIdentityProvider
import java.io.IOException
import javax.inject.Inject
import okhttp3.Call
import okhttp3.Interceptor
import okhttp3.Request
import okhttp3.Response

class SessionRequestContextBinder
@Inject
constructor(
    private val tokenVault: TokenVault,
    private val sessionIdentityProvider: SessionIdentityProvider,
    private val serverRuntimeCoordinator: ServerRuntimeCoordinator,
    private val serverConfigRepository: ServerConfigRepository,
) {
    fun bind(request: Request): Request {
        if (request.tag(SessionRequestContext::class.java) != null) return request
        return request
            .newBuilder()
            .tag(SessionRequestContext::class.java, captureContext())
            .build()
    }

    private fun captureContext(): SessionRequestContext {
        val generation = serverRuntimeCoordinator.captureGeneration()
        val endpoint =
            serverConfigRepository.currentEndpoint()
                ?: throw IOException("Server endpoint is not configured")
        val identityBeforeRead = sessionIdentityProvider.activeIdentity()
        val tokens = tokenVault.read()
        val identityAfterRead = sessionIdentityProvider.activeIdentity()
        serverRuntimeCoordinator.requireCurrent(generation)
        val identity =
            identityBeforeRead?.takeIf {
                it == identityAfterRead &&
                    it.serverGeneration == generation &&
                    tokens != null &&
                    tokens.userId == it.userId &&
                    tokens.sessionId == it.sessionId
            }
        return SessionRequestContext(
            userId = identity?.userId,
            sessionId = identity?.sessionId,
            serverGeneration = generation,
            serverEndpoint = endpoint,
            accessToken = identity?.let { checkNotNull(tokens).accessToken },
        )
    }
}

class SessionRequestContextCallFactory(
    private val delegate: Call.Factory,
    private val contextBinder: SessionRequestContextBinder,
) : Call.Factory {
    override fun newCall(request: Request): Call = delegate.newCall(contextBinder.bind(request))
}

class SessionRequestContextInterceptor
@Inject
constructor(private val contextBinder: SessionRequestContextBinder) :
    Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response = chain.proceed(contextBinder.bind(chain.request()))
}

class SessionRequestContextValidationInterceptor
@Inject
constructor(
    private val tokenVault: TokenVault,
    private val sessionIdentityProvider: SessionIdentityProvider,
    private val serverRuntimeCoordinator: ServerRuntimeCoordinator,
) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val context =
            chain.request().tag(SessionRequestContext::class.java)
                ?: throw StaleSessionRequestException()
        if (!context.isCurrent(tokenVault, sessionIdentityProvider, serverRuntimeCoordinator)) {
            throw StaleSessionRequestException()
        }
        return chain.proceed(chain.request())
    }
}

class ServerEndpointInterceptor
@Inject
constructor(
    private val serverConfigRepository: ServerConfigRepository,
    private val serverRuntimeCoordinator: ServerRuntimeCoordinator = ServerRuntimeCoordinator(),
) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val request = chain.request()
        val boundServer =
            request.tag(ExpectedServerRequestContext::class.java)
                ?: request.tag(SessionRequestContext::class.java)?.let {
                    ExpectedServerRequestContext(it.serverEndpoint, it.serverGeneration)
                }
        val generation = boundServer?.generation ?: serverRuntimeCoordinator.captureGeneration()
        val currentEndpoint = resolveCurrentEndpoint(boundServer)
        serverRuntimeCoordinator.requireCurrent(generation)
        val endpoint = boundServer?.endpoint ?: currentEndpoint
        requireCurrentEndpoint(boundServer, currentEndpoint, endpoint)
        val rewrittenUrl =
            request.url
                .newBuilder()
                .scheme(endpoint.protocol.scheme)
                .host(endpoint.host)
                .port(endpoint.port)
                .build()
        val response =
            chain.proceed(
                request
                    .newBuilder()
                    .url(rewrittenUrl)
                    .tag(
                        ServerRequestContext::class.java,
                        ServerRequestContext(endpoint, generation),
                    ).build(),
            )
        return requireCurrentGeneration(response, generation)
    }

    private fun resolveCurrentEndpoint(boundServer: ExpectedServerRequestContext?): ServerEndpoint =
        serverConfigRepository.currentEndpoint() ?: if (boundServer != null) {
            throw StaleServerGenerationException()
        } else {
            throw IOException("Server endpoint is not configured")
        }

    private fun requireCurrentEndpoint(
        boundServer: ExpectedServerRequestContext?,
        currentEndpoint: ServerEndpoint,
        requestEndpoint: ServerEndpoint,
    ) {
        if (boundServer != null && currentEndpoint != requestEndpoint) {
            throw StaleServerGenerationException()
        }
    }

    private fun requireCurrentGeneration(response: Response, generation: ServerGeneration): Response {
        try {
            serverRuntimeCoordinator.requireCurrent(generation)
        } catch (failure: IOException) {
            response.close()
            throw failure
        }
        return response
    }
}

class ClientMetadataInterceptor
@Inject
constructor() : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val request =
            chain
                .request()
                .newBuilder()
                .header(HEADER_ACCEPT, MEDIA_TYPE_JSON)
                .header(HEADER_PLATFORM, PLATFORM_ANDROID)
                .header(HEADER_APP_VERSION, BuildConfig.VERSION_NAME)
                .build()
        return chain.proceed(request)
    }

    private companion object {
        const val HEADER_ACCEPT = "Accept"
        const val HEADER_PLATFORM = "X-Client-Platform"
        const val HEADER_APP_VERSION = "X-Client-Version"
        const val MEDIA_TYPE_JSON = "application/json"
        const val PLATFORM_ANDROID = "ANDROID"
    }
}

class BearerTokenInterceptor
@Inject
constructor(
    private val tokenVault: TokenVault,
    private val serverConfigRepository: ServerConfigRepository,
    private val serverRuntimeCoordinator: ServerRuntimeCoordinator = ServerRuntimeCoordinator(),
    private val sessionIdentityProvider: SessionIdentityProvider =
        SessionIdentityProvider {
            tokenVault.read()?.let { tokens ->
                ActiveSessionIdentity(
                    tokens.userId,
                    tokens.sessionId,
                    serverRuntimeCoordinator.captureGeneration(),
                )
            }
        },
) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val unboundRequest = chain.request()
        val sessionContext =
            unboundRequest.tag(SessionRequestContext::class.java)
                ?: captureFallbackContext()
        val originalRequest =
            if (unboundRequest.tag(SessionRequestContext::class.java) == null) {
                unboundRequest
                    .newBuilder()
                    .tag(SessionRequestContext::class.java, sessionContext)
                    .build()
            } else {
                unboundRequest
            }
        val requestBuilder = originalRequest.newBuilder()
        val endpoint = serverConfigRepository.currentEndpoint()
        val requestContext = originalRequest.tag(ServerRequestContext::class.java)
        val generation = requestContext?.generation ?: sessionContext.serverGeneration
        serverRuntimeCoordinator.requireCurrent(generation)
        val isApiOrigin =
            endpoint != null &&
                originalRequest.url.scheme == endpoint.protocol.scheme &&
                originalRequest.url.host == endpoint.host &&
                originalRequest.url.port == endpoint.port
        val isCurrentServer = requestContext == null || requestContext.endpoint == endpoint
        val isCurrentSession =
            sessionContext.serverGeneration == generation &&
                sessionContext.isCurrent(
                    tokenVault,
                    sessionIdentityProvider,
                    serverRuntimeCoordinator,
                )

        if (
            isApiOrigin &&
            isCurrentServer &&
            isCurrentSession &&
            sessionContext.accessToken != null
        ) {
            requestBuilder
                .header(AUTHORIZATION_HEADER, "Bearer " + sessionContext.accessToken.value)
        } else {
            requestBuilder.removeHeader(AUTHORIZATION_HEADER)
        }
        return chain.proceed(requestBuilder.build())
    }

    private fun captureFallbackContext(): SessionRequestContext {
        val generation = serverRuntimeCoordinator.captureGeneration()
        val endpoint =
            serverConfigRepository.currentEndpoint()
                ?: throw IOException("Server endpoint is not configured")
        val identity = sessionIdentityProvider.activeIdentity()
        val tokens = tokenVault.read()
        serverRuntimeCoordinator.requireCurrent(generation)
        val isConsistent =
            identity != null &&
                tokens != null &&
                identity.serverGeneration == generation &&
                tokens.userId == identity.userId &&
                tokens.sessionId == identity.sessionId
        return SessionRequestContext(
            userId = identity?.userId?.takeIf { isConsistent },
            sessionId = identity?.sessionId?.takeIf { isConsistent },
            serverGeneration = generation,
            serverEndpoint = endpoint,
            accessToken = tokens?.accessToken?.takeIf { isConsistent },
        )
    }
}

data class SessionRequestContext(
    val userId: String?,
    val sessionId: String?,
    val serverGeneration: ServerGeneration,
    val serverEndpoint: ServerEndpoint,
    val accessToken: AccessToken?,
) {
    fun identityOrNull(): ActiveSessionIdentity? {
        val boundUserId = userId ?: return null
        val boundSessionId = sessionId ?: return null
        if (accessToken == null) return null
        return ActiveSessionIdentity(boundUserId, boundSessionId, serverGeneration)
    }

    fun isCurrent(
        tokenVault: TokenVault,
        sessionIdentityProvider: SessionIdentityProvider,
        serverRuntimeCoordinator: ServerRuntimeCoordinator,
    ): Boolean {
        if (!serverRuntimeCoordinator.isCurrent(serverGeneration)) return false
        val expectedIdentity = identityOrNull()
        val identityBeforeRead = sessionIdentityProvider.activeIdentity()
        val tokens = tokenVault.read()
        val identityAfterRead = sessionIdentityProvider.activeIdentity()
        if (expectedIdentity == null) {
            return identityBeforeRead == null && identityAfterRead == null && tokens == null
        }
        return identityBeforeRead == expectedIdentity &&
            identityAfterRead == expectedIdentity &&
            tokens != null &&
            tokens.userId == expectedIdentity.userId &&
            tokens.sessionId == expectedIdentity.sessionId
    }
}

internal data class ServerRequestContext(val endpoint: ServerEndpoint, val generation: ServerGeneration)

internal data class ExpectedServerRequestContext(val endpoint: ServerEndpoint, val generation: ServerGeneration)

internal class StaleSessionRequestException :
    IOException(
        "Request belongs to a previous session",
    )

class RemoveAuthorizationInterceptor
@Inject
constructor() : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response = chain.proceed(
        chain
            .request()
            .newBuilder()
            .removeHeader(AUTHORIZATION_HEADER)
            .build(),
    )
}

internal const val AUTHORIZATION_HEADER = "Authorization"
