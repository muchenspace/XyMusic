package com.xymusic.app.feature.settings.data

import com.xymusic.app.core.common.IoDispatcher
import com.xymusic.app.core.data.media.remote.ArtworkDto
import com.xymusic.app.core.model.media.Artwork
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.network.MediaHttpClient
import com.xymusic.app.core.network.ServerGeneration
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.network.model.ProblemCode
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionInvalidator
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.data.network.ProblemResponseParser
import com.xymusic.app.data.network.auth.IdempotencyKeyGenerator
import com.xymusic.app.feature.settings.data.remote.CreateAvatarUploadRequestDto
import com.xymusic.app.feature.settings.data.remote.CurrentUserDto
import com.xymusic.app.feature.settings.data.remote.ProfileApi
import com.xymusic.app.feature.settings.domain.ProfileRepository
import com.xymusic.app.feature.settings.domain.SettingsResult
import com.xymusic.app.feature.settings.domain.model.AvatarUploadCommand
import com.xymusic.app.feature.settings.domain.model.ProfileValueChange
import com.xymusic.app.feature.settings.domain.model.UpdateProfileCommand
import com.xymusic.app.feature.settings.domain.model.UserProfile
import com.xymusic.app.feature.settings.domain.model.UserRole
import com.xymusic.app.feature.settings.domain.model.UserStatus
import java.io.IOException
import java.security.MessageDigest
import java.time.Instant
import java.util.concurrent.CancellationException
import javax.inject.Inject
import javax.inject.Singleton
import kotlin.coroutines.resumeWithException
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import okhttp3.Call
import okhttp3.Callback
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response as OkHttpResponse
import retrofit2.Response

@Singleton
@OptIn(ExperimentalCoroutinesApi::class)
class DefaultProfileRepository
@Inject
constructor(
    private val api: ProfileApi,
    private val problemResponseParser: ProblemResponseParser,
    private val idempotencyKeyGenerator: IdempotencyKeyGenerator,
    private val sessionProvider: AppSessionProvider,
    private val sessionInvalidator: SessionInvalidator,
    private val sessionMutationCoordinator: SessionMutationCoordinator,
    private val profileMemoryCache: ProfileMemoryCache,
    private val serverRuntimeCoordinator: ServerRuntimeCoordinator,
    @MediaHttpClient private val mediaHttpClient: OkHttpClient,
    @IoDispatcher private val ioDispatcher: CoroutineDispatcher,
) : ProfileRepository {
    private val profileLoadMutex = Mutex()

    override val profile: Flow<UserProfile?> =
        sessionProvider.sessionState.flatMapLatest { state ->
            if (state is AppSessionState.SignedIn) {
                profileMemoryCache.observe(state.userId)
            } else {
                flowOf(null)
            }
        }

    override suspend fun ensureLoaded(): SettingsResult<UserProfile> = ioCall {
        val owner = requireOwner()
        profileMemoryCache.current(owner)?.let { return@ioCall SettingsResult.Success(it) }
        profileLoadMutex.withLock {
            val currentOwner = requireOwner()
            profileMemoryCache.current(currentOwner)?.let { SettingsResult.Success(it) }
                ?: fetchCurrentProfile()
        }
    }

    override suspend fun refresh(): SettingsResult<UserProfile> = ioCall {
        profileLoadMutex.withLock { fetchCurrentProfile() }
    }

    private suspend fun fetchCurrentProfile(): SettingsResult<UserProfile> {
        val generation = serverRuntimeCoordinator.captureGeneration()
        val expectedOwner = requireOwner()
        val profile = body(api.currentUser()).toDomain()
        require(profile.id == expectedOwner) { "Profile owner does not match the active session" }
        profileMemoryCache.put(expectedOwner, generation, profile)
        return SettingsResult.Success(profile)
    }

    override suspend fun update(command: UpdateProfileCommand): SettingsResult<UserProfile> = ioCall {
        val generation = serverRuntimeCoordinator.captureGeneration()
        val expectedOwner = requireOwner()
        val request =
            buildJsonObject {
                put("expectedVersion", JsonPrimitive(command.expectedVersion))
                when (val change = command.displayName) {
                    ProfileValueChange.Unchanged -> Unit
                    is ProfileValueChange.Set -> put("displayName", JsonPrimitive(change.value))
                }
                when (val change = command.bio) {
                    ProfileValueChange.Unchanged -> Unit
                    is ProfileValueChange.Set ->
                        put(
                            "bio",
                            change.value?.let(::JsonPrimitive) ?: JsonNull,
                        )
                }
            }
        val profile =
            body(
                api.updateCurrentUser(idempotencyKeyGenerator.generate(), request),
            ).toDomain()
        require(profile.id == expectedOwner) { "Profile owner does not match the active session" }
        profileMemoryCache.put(expectedOwner, generation, profile)
        SettingsResult.Success(profile)
    }

    override suspend fun uploadAvatar(command: AvatarUploadCommand): SettingsResult<UserProfile> = ioCall {
        val generation = serverRuntimeCoordinator.captureGeneration()
        val expectedOwner = requireOwner()
        val checksum =
            MessageDigest
                .getInstance("SHA-256")
                .digest(command.content)
                .joinToString("") { byte -> "%02x".format(byte) }
        val upload =
            body(
                api.createAvatarUpload(
                    idempotencyKeyGenerator.generate(),
                    CreateAvatarUploadRequestDto(
                        fileName = command.fileName,
                        contentType = command.contentType,
                        sizeBytes = command.content.size,
                        checksumSha256 = checksum,
                    ),
                ),
            )
        val request =
            Request
                .Builder()
                .url(upload.uploadUrl)
                .put(command.content.toRequestBody(command.contentType.toMediaType()))
                .apply {
                    upload.requiredHeaders
                        .filterKeys { !it.equals("content-length", ignoreCase = true) }
                        .forEach(::header)
                }.build()
        requireUploadContext(generation, expectedOwner)
        executeAvatarUpload(request, generation, expectedOwner).use { response ->
            if (!response.isSuccessful) throw IOException("Avatar upload failed with HTTP ${response.code}")
        }
        requireUploadContext(generation, expectedOwner)
        val profile =
            body(
                api.completeAvatarUpload(upload.id, idempotencyKeyGenerator.generate()),
            ).toDomain()
        requireUploadContext(generation, expectedOwner)
        require(profile.id == expectedOwner) { "Profile owner does not match the active session" }
        profileMemoryCache.put(expectedOwner, generation, profile)
        SettingsResult.Success(profile)
    }

    private suspend fun executeAvatarUpload(
        request: Request,
        generation: ServerGeneration,
        expectedOwner: String,
    ): OkHttpResponse = coroutineScope {
        val call = mediaHttpClient.newCall(request)
        val serverWatcher =
            launch(start = CoroutineStart.UNDISPATCHED) {
                serverRuntimeCoordinator.state.first {
                    !serverRuntimeCoordinator.isCurrent(generation)
                }
                call.cancel()
            }
        val sessionWatcher =
            launch(start = CoroutineStart.UNDISPATCHED) {
                sessionProvider.sessionState.first { state ->
                    state !is AppSessionState.SignedIn || state.userId != expectedOwner
                }
                call.cancel()
            }
        try {
            call.awaitResponse()
        } catch (failure: IOException) {
            if (!isUploadContextCurrent(generation, expectedOwner)) {
                throw CancellationException("Avatar upload session changed").apply {
                    initCause(failure)
                }
            }
            throw failure
        } finally {
            serverWatcher.cancel()
            sessionWatcher.cancel()
        }
    }

    override suspend fun logoutAllSessions(): SettingsResult<Unit> = withContext(ioDispatcher) {
        try {
            val generation = serverRuntimeCoordinator.captureGeneration()
            val owner = requireOwner()
            val response = api.logoutAllSessions()
            if (!response.isSuccessful) return@withContext failure(response)
            var cleanupFailure: Throwable? = null
            withContext(NonCancellable) {
                sessionMutationCoordinator.mutate {
                    serverRuntimeCoordinator.requireCurrent(generation)
                    profileMemoryCache.clear()
                    runCatching { sessionInvalidator.invalidateSession(owner) }
                        .onFailure { cleanupFailure = it }
                }
            }
            if (cleanupFailure == null) {
                SettingsResult.Success(Unit)
            } else {
                protocolFailure("All sessions were revoked, but local cleanup failed")
            }
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: SignedOutException) {
            SettingsResult.Failure(authenticationFailure())
        } catch (failure: ProfileRemoteException) {
            SettingsResult.Failure(failure.error)
        } catch (_: IOException) {
            SettingsResult.Failure(DomainError.Network("Unable to reach the server"))
        } catch (_: Exception) {
            protocolFailure("Unable to revoke all sessions")
        }
    }

    private suspend fun <T> ioCall(block: suspend () -> SettingsResult<T>): SettingsResult<T> =
        withContext(ioDispatcher) {
            try {
                block()
            } catch (failure: CancellationException) {
                throw failure
            } catch (_: SignedOutException) {
                SettingsResult.Failure(authenticationFailure())
            } catch (failure: ProfileRemoteException) {
                SettingsResult.Failure(failure.error)
            } catch (_: IOException) {
                SettingsResult.Failure(DomainError.Network("Unable to reach the server"))
            } catch (_: Exception) {
                protocolFailure("Invalid profile response")
            }
        }

    private fun <T> body(response: Response<T>): T {
        if (!response.isSuccessful) throw ProfileRemoteException(failure(response).error)
        return response.body() ?: throw ProfileProtocolException("Profile response body is missing")
    }

    private fun failure(response: Response<*>): SettingsResult.Failure = SettingsResult.Failure(
        problemResponseParser.parse(
            response.code(),
            response.errorBody()?.string(),
            response.headers()[TRACE_ID_HEADER],
            response.headers()[RETRY_AFTER_HEADER]?.toLongOrNull(),
        ),
    )

    private fun CurrentUserDto.toDomain(): UserProfile = UserProfile(
        id = id,
        username = username,
        displayName = displayName,
        bio = bio,
        avatar = avatar.toDomain(),
        role = UserRole.valueOf(role),
        status = UserStatus.valueOf(status),
        version = version,
        createdAtEpochMillis = Instant.parse(createdAt).toEpochMilli(),
        updatedAtEpochMillis = Instant.parse(updatedAt).toEpochMilli(),
    )

    private fun ArtworkDto?.toDomain(): Artwork? = this?.let {
        Artwork(
            assetId,
            url,
            cacheKey,
            mimeType,
            expiresAt?.let { value -> Instant.parse(value).toEpochMilli() },
            width,
            height,
        )
    }

    private fun requireOwner(): String = (sessionProvider.sessionState.value as? AppSessionState.SignedIn)?.userId
        ?: throw SignedOutException

    private fun requireUploadContext(generation: ServerGeneration, expectedOwner: String) {
        serverRuntimeCoordinator.requireCurrent(generation)
        if (!isUploadContextCurrent(generation, expectedOwner)) {
            throw CancellationException("Avatar upload session changed")
        }
    }

    private fun isUploadContextCurrent(generation: ServerGeneration, expectedOwner: String): Boolean =
        serverRuntimeCoordinator.isCurrent(generation) &&
            (sessionProvider.sessionState.value as? AppSessionState.SignedIn)?.userId == expectedOwner

    private fun authenticationFailure() = DomainError.Authentication(
        "Authentication is required",
        null,
        ProblemCode.AuthenticationRequired,
    )

    private fun protocolFailure(detail: String) = SettingsResult.Failure(
        DomainError.Protocol(detail, null, null),
    )

    private object SignedOutException : IllegalStateException()

    private class ProfileRemoteException(val error: DomainError) : IOException()

    private class ProfileProtocolException(message: String) : IllegalStateException(message)

    private companion object {
        const val TRACE_ID_HEADER = "X-Trace-Id"
        const val RETRY_AFTER_HEADER = "Retry-After"
    }
}

private suspend fun Call.awaitResponse(): OkHttpResponse = suspendCancellableCoroutine { continuation ->
    continuation.invokeOnCancellation { cancel() }
    enqueue(
        object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                if (continuation.isActive) continuation.resumeWithException(e)
            }

            override fun onResponse(call: Call, response: OkHttpResponse) {
                if (!continuation.isActive) {
                    response.close()
                    return
                }
                continuation.resume(response) { _, unconsumedResponse, _ ->
                    unconsumedResponse.close()
                }
            }
        },
    )
}
