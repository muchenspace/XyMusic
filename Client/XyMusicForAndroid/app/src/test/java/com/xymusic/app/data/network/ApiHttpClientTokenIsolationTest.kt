package com.xymusic.app.data.network

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.security.AccessToken
import com.xymusic.app.core.security.RefreshToken
import com.xymusic.app.core.security.SessionTokens
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.support.InMemoryServerConfigRepository
import com.xymusic.app.support.InMemoryTokenVault
import java.io.IOException
import java.util.concurrent.atomic.AtomicReference
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Test

class ApiHttpClientTokenIsolationTest {
    @Test
    fun callQueuedByOneSessionIsRejectedAfterAnotherSessionBecomesActive() {
        val apiServer = MockWebServer()
        apiServer.enqueue(MockResponse().setResponseCode(200))
        apiServer.start()
        try {
            val runtime = ServerRuntimeCoordinator()
            val sessionA =
                tokens(
                    userId = "user-a",
                    sessionId = "session-a",
                    accessToken = "access-a",
                )
            val sessionB =
                tokens(
                    userId = "user-b",
                    sessionId = "session-b",
                    accessToken = "access-b",
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
            val serverRepository = InMemoryServerConfigRepository.from(apiServer.url("/"))
            val delegate =
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
                    ).build()
            val callFactory =
                SessionRequestContextCallFactory(
                    delegate = delegate,
                    contextBinder =
                    SessionRequestContextBinder(
                        tokenVault = vault,
                        sessionIdentityProvider = identityProvider,
                        serverRuntimeCoordinator = runtime,
                        serverConfigRepository = serverRepository,
                    ),
                )

            val queuedCall =
                callFactory.newCall(
                    Request.Builder().url(apiServer.url("/me")).build(),
                )
            vault.write(sessionB)
            identity.set(
                ActiveSessionIdentity(
                    sessionB.userId,
                    sessionB.sessionId,
                    runtime.captureGeneration(),
                ),
            )

            val failure = runCatching { queuedCall.execute().close() }.exceptionOrNull()

            assertThat(failure).isInstanceOf(IOException::class.java)
            assertThat(failure).isInstanceOf(StaleSessionRequestException::class.java)
            assertThat(apiServer.requestCount).isEqualTo(0)
        } finally {
            apiServer.shutdown()
        }
    }

    @Test
    fun bearerTokenIsOnlyAttachedToConfiguredApiOrigin() {
        val apiServer = MockWebServer()
        val externalServer = MockWebServer()
        apiServer.enqueue(MockResponse().setResponseCode(200))
        externalServer.enqueue(MockResponse().setResponseCode(200))
        apiServer.start()
        externalServer.start()
        try {
            val client =
                OkHttpClient
                    .Builder()
                    .addInterceptor(
                        BearerTokenInterceptor(
                            tokenVault = InMemoryTokenVault(tokens()),
                            serverConfigRepository = InMemoryServerConfigRepository.from(apiServer.url("/")),
                        ),
                    ).build()

            client
                .newCall(Request.Builder().url(apiServer.url("/me")).build())
                .execute()
                .close()
            client
                .newCall(
                    Request
                        .Builder()
                        .url(externalServer.url("/signed-media"))
                        .header("Authorization", "Bearer manually-added")
                        .build(),
                ).execute()
                .close()

            assertThat(apiServer.takeRequest().getHeader("Authorization"))
                .isEqualTo("Bearer access-token")
            assertThat(externalServer.takeRequest().getHeader("Authorization")).isNull()
        } finally {
            apiServer.shutdown()
            externalServer.shutdown()
        }
    }

    @Test
    fun tokenFromPreviousServerGenerationIsNotAttached() {
        val apiServer = MockWebServer()
        apiServer.enqueue(MockResponse().setResponseCode(200))
        apiServer.start()
        try {
            val vault = InMemoryTokenVault(tokens())
            val runtime = ServerRuntimeCoordinator()
            val oldIdentity =
                ActiveSessionIdentity(
                    userId = "user-1",
                    sessionId = "session-1",
                    serverGeneration = runtime.captureGeneration(),
                )
            val switchingGeneration = runtime.beginSwitch()
            runtime.finishSwitch(switchingGeneration)
            val client =
                OkHttpClient
                    .Builder()
                    .addInterceptor(
                        BearerTokenInterceptor(
                            tokenVault = vault,
                            serverConfigRepository = InMemoryServerConfigRepository.from(apiServer.url("/")),
                            sessionIdentityProvider = SessionIdentityProvider { oldIdentity },
                            serverRuntimeCoordinator = runtime,
                        ),
                    ).build()

            client
                .newCall(Request.Builder().url(apiServer.url("/me")).build())
                .execute()
                .close()

            assertThat(apiServer.takeRequest().getHeader("Authorization")).isNull()
        } finally {
            apiServer.shutdown()
        }
    }

    private fun tokens(
        userId: String = "user-1",
        sessionId: String = "session-1",
        accessToken: String = "access-token",
    ): SessionTokens = SessionTokens(
        userId = userId,
        sessionId = sessionId,
        accessToken = AccessToken.from(accessToken),
        accessTokenExpiresAtEpochMillis = Long.MAX_VALUE,
        refreshToken = RefreshToken.from("refresh-token"),
        refreshTokenExpiresAtEpochMillis = Long.MAX_VALUE,
    )
}
