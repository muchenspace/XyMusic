package com.xymusic.app.feature.settings.data

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.network.ProblemMapper
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionInvalidator
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.data.network.ProblemResponseParser
import com.xymusic.app.data.network.auth.IdempotencyKeyGenerator
import com.xymusic.app.feature.settings.data.remote.AvatarUploadDto
import com.xymusic.app.feature.settings.data.remote.CompleteAvatarUploadRequestDto
import com.xymusic.app.feature.settings.data.remote.CreateAvatarUploadRequestDto
import com.xymusic.app.feature.settings.data.remote.CurrentUserDto
import com.xymusic.app.feature.settings.data.remote.ProfileApi
import com.xymusic.app.domain.server.ServerConfigRepository
import com.xymusic.app.feature.settings.domain.SettingsResult
import com.xymusic.app.feature.settings.domain.model.AvatarUploadCommand
import com.xymusic.app.support.InMemoryServerConfigRepository
import java.util.concurrent.CancellationException
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeout
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.SocketPolicy
import org.junit.Test
import retrofit2.Response

class DefaultProfileRepositoryTest {
    @Test
    fun concurrentEnsureLoadedCallsShareOneRequestAndReuseTheCache() = runTest {
        val requestStarted = CompletableDeferred<Unit>()
        val releaseResponse = CompletableDeferred<Unit>()
        val runtime = ServerRuntimeCoordinator()
        val session = MutableSessionProvider(AppSessionState.SignedIn(USER_ID))
        val api =
            FakeProfileApi().apply {
                currentUserResponse = {
                    requestStarted.complete(Unit)
                    releaseResponse.await()
                    Response.success(PROFILE)
                }
            }
        val repository = repository(api, session, runtime)
        val first = backgroundScope.async(Dispatchers.IO) { repository.ensureLoaded() }
        requestStarted.await()
        val second = backgroundScope.async(Dispatchers.IO) { repository.ensureLoaded() }

        releaseResponse.complete(Unit)

        assertThat(first.await()).isEqualTo(second.await())
        assertThat(api.currentUserCalls).isEqualTo(1)
        assertThat(repository.ensureLoaded()).isInstanceOf(SettingsResult.Success::class.java)
        assertThat(api.currentUserCalls).isEqualTo(1)
    }

    @Test
    fun configuredServerLocalAvatarUploadUsesAuthenticatedApiClient() = runTest {
        val server = MockWebServer()
        val uploadPath = "/api/v1/users/me/avatar/uploads/22222222-2222-4222-8222-222222222222"
        server.start()
        server.enqueue(MockResponse().setResponseCode(200))
        try {
            val runtime = ServerRuntimeCoordinator()
            val session = MutableSessionProvider(AppSessionState.SignedIn(USER_ID))
            val api = FakeProfileApi(server).apply { avatarUploadPath = uploadPath }
            val apiClient =
                OkHttpClient
                    .Builder()
                    .addInterceptor { chain ->
                        chain.proceed(
                            chain.request()
                                .newBuilder()
                                .header("Authorization", "Bearer local-session")
                                .build(),
                        )
                    }.build()
            val mediaClient = OkHttpClient.Builder().build()
            val result =
                repository(
                    api = api,
                    session = session,
                    runtime = runtime,
                    serverConfigRepository = InMemoryServerConfigRepository.from(server.url("/")),
                    mediaHttpClient = mediaClient,
                    apiHttpClient = apiClient,
                ).uploadAvatar(AVATAR)

            assertThat(result).isInstanceOf(SettingsResult.Success::class.java)
            val request = checkNotNull(server.takeRequest(5, TimeUnit.SECONDS))
            assertThat(request.path).isEqualTo(uploadPath)
            assertThat(request.getHeader("Authorization")).isEqualTo("Bearer local-session")
            assertThat(api.completeCalls).isEqualTo(1)
        } finally {
            server.shutdown()
        }
    }

    @Test
    fun coroutineCancellationStopsInFlightAvatarUpload() = runTest {
        val server = unresponsiveServer()
        val runtime = ServerRuntimeCoordinator()
        val session = MutableSessionProvider(AppSessionState.SignedIn(USER_ID))
        val api = FakeProfileApi(server)
        val upload =
            backgroundScope.async(Dispatchers.IO) {
                repository(api, session, runtime).uploadAvatar(AVATAR)
            }
        try {
            assertThat(takeRequest(server)).isNotNull()

            withContext(Dispatchers.IO) {
                withTimeout(5_000) {
                    upload.cancel(CancellationException("screen closed"))
                    upload.cancelAndJoin()
                }
            }

            assertThat(upload.isCancelled).isTrue()
            assertThat(api.completeCalls).isEqualTo(0)
        } finally {
            upload.cancel()
            server.shutdown()
        }
    }

    @Test
    fun serverSwitchStopsInFlightAvatarUpload() = runTest {
        val server = unresponsiveServer()
        val runtime = ServerRuntimeCoordinator()
        val session = MutableSessionProvider(AppSessionState.SignedIn(USER_ID))
        val api = FakeProfileApi(server)
        val upload =
            backgroundScope.async(Dispatchers.IO) {
                repository(api, session, runtime).uploadAvatar(AVATAR)
            }
        try {
            assertThat(takeRequest(server)).isNotNull()

            runtime.beginSwitch()
            val failure =
                withContext(Dispatchers.IO) {
                    withTimeout(5_000) {
                        runCatching { upload.await() }.exceptionOrNull()
                    }
                }

            assertThat(failure).isInstanceOf(CancellationException::class.java)
            assertThat(api.completeCalls).isEqualTo(0)
        } finally {
            upload.cancel()
            server.shutdown()
        }
    }

    @Test
    fun accountSwitchStopsInFlightAvatarUpload() = runTest {
        val server = unresponsiveServer()
        val runtime = ServerRuntimeCoordinator()
        val session = MutableSessionProvider(AppSessionState.SignedIn(USER_ID))
        val api = FakeProfileApi(server)
        val upload =
            backgroundScope.async(Dispatchers.IO) {
                repository(api, session, runtime).uploadAvatar(AVATAR)
            }
        try {
            assertThat(takeRequest(server)).isNotNull()

            session.sessionState.value = AppSessionState.SignedIn("replacement-user")
            val failure =
                withContext(Dispatchers.IO) {
                    withTimeout(5_000) {
                        runCatching { upload.await() }.exceptionOrNull()
                    }
                }

            assertThat(failure).isInstanceOf(CancellationException::class.java)
            assertThat(api.completeCalls).isEqualTo(0)
        } finally {
            upload.cancel()
            server.shutdown()
        }
    }

    private fun repository(
        api: ProfileApi,
        session: AppSessionProvider,
        runtime: ServerRuntimeCoordinator,
        serverConfigRepository: ServerConfigRepository = InMemoryServerConfigRepository(null),
        mediaHttpClient: OkHttpClient =
            OkHttpClient
                .Builder()
                .readTimeout(1, TimeUnit.DAYS)
                .writeTimeout(1, TimeUnit.DAYS)
                .callTimeout(0, TimeUnit.MILLISECONDS)
                .build(),
        apiHttpClient: OkHttpClient =
            OkHttpClient
                .Builder()
                .readTimeout(1, TimeUnit.DAYS)
                .writeTimeout(1, TimeUnit.DAYS)
                .callTimeout(0, TimeUnit.MILLISECONDS)
                .build(),
    ) =
        DefaultProfileRepository(
            api = api,
            problemResponseParser = ProblemResponseParser(Json, ProblemMapper()),
            idempotencyKeyGenerator = IdempotencyKeyGenerator { "test-idempotency-key" },
            sessionProvider = session,
            sessionInvalidator = SessionInvalidator { },
            sessionMutationCoordinator = SessionMutationCoordinator(),
            profileMemoryCache = ProfileMemoryCache(runtime),
            serverRuntimeCoordinator = runtime,
            serverConfigRepository = serverConfigRepository,
            mediaHttpClient = mediaHttpClient,
            apiHttpClient = apiHttpClient,
            ioDispatcher = Dispatchers.IO,
        )

    private fun unresponsiveServer(): MockWebServer = MockWebServer().apply {
        enqueue(MockResponse().setSocketPolicy(SocketPolicy.NO_RESPONSE))
        start()
    }

    private suspend fun takeRequest(server: MockWebServer) = withContext(Dispatchers.IO) {
        server.takeRequest(5, TimeUnit.SECONDS)
    }

    private class MutableSessionProvider(initialState: AppSessionState) : AppSessionProvider {
        override val sessionState = MutableStateFlow(initialState)

        override suspend fun restoreSession() = Unit
    }

    private class FakeProfileApi(private val server: MockWebServer? = null) : ProfileApi {
        var completeCalls = 0
        var currentUserCalls = 0
        var currentUserResponse: suspend () -> Response<CurrentUserDto> = { Response.success(PROFILE) }
        var avatarUploadPath = "/avatar"

        override suspend fun currentUser(): Response<CurrentUserDto> {
            currentUserCalls += 1
            return currentUserResponse()
        }

        override suspend fun updateCurrentUser(idempotencyKey: String, request: JsonObject): Response<CurrentUserDto> =
            error("Not used")

        override suspend fun createAvatarUpload(
            idempotencyKey: String,
            request: CreateAvatarUploadRequestDto,
        ): Response<AvatarUploadDto> = Response.success(
            AvatarUploadDto(
                id = "upload-1",
                uploadUrl = checkNotNull(server).url(avatarUploadPath).toString(),
                requiredHeaders = emptyMap(),
            ),
        )

        override suspend fun completeAvatarUpload(
            uploadId: String,
            idempotencyKey: String,
            request: CompleteAvatarUploadRequestDto,
        ): Response<CurrentUserDto> {
            completeCalls += 1
            return Response.success(PROFILE)
        }

        override suspend fun logoutAllSessions(): Response<Unit> = error("Not used")
    }

    private companion object {
        const val USER_ID = "11111111-1111-4111-8111-111111111111"
        val AVATAR =
            AvatarUploadCommand(
                fileName = "avatar.png",
                contentType = "image/png",
                content = byteArrayOf(1, 2, 3),
            )
        val PROFILE =
            CurrentUserDto(
                id = USER_ID,
                username = "alice",
                displayName = "Alice",
                bio = null,
                avatar = null,
                role = "USER",
                status = "ACTIVE",
                version = 1,
                createdAt = "2026-07-10T00:00:00Z",
                updatedAt = "2026-07-10T00:00:00Z",
            )
    }
}
