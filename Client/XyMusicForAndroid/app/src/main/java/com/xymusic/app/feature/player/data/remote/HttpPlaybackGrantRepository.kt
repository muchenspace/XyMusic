package com.xymusic.app.feature.player.data.remote

import com.xymusic.app.core.network.ServerSynchronizedClock
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.data.network.ProblemResponseParser
import com.xymusic.app.domain.server.ServerConfigRepository
import com.xymusic.app.feature.player.data.media.PlaybackGrantKey
import com.xymusic.app.feature.player.data.media.PlaybackGrantRegistry
import com.xymusic.app.feature.player.data.media.PlaybackGrantStore
import com.xymusic.app.feature.player.domain.PlaybackGrant
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.model.PlayerFailure
import java.net.URI
import java.time.Instant
import java.time.ZonedDateTime
import java.time.format.DateTimeFormatter
import java.util.UUID
import java.util.concurrent.CancellationException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

@Singleton
class HttpPlaybackGrantRepository
@Inject
constructor(
    private val api: PlaybackApi,
    private val store: PlaybackGrantStore,
    private val problemResponseParser: ProblemResponseParser,
    private val serverConfigRepository: ServerConfigRepository,
    private val sessionIdentityProvider: SessionIdentityProvider,
    private val clock: ServerSynchronizedClock,
    private val grantRegistry: PlaybackGrantRegistry,
) : PlaybackGrantRepository {
    private val grantMutexes = Array(GRANT_MUTEX_COUNT) { Mutex() }
    private val storeLock = Any()
    private var storeGeneration = 0L
    private var storeIdentity: ActiveSessionIdentity? = null

    override suspend fun get(
        trackId: String,
        forceRefresh: Boolean,
    ): PlayerResult<PlaybackGrant> {
        val identity =
            sessionIdentityProvider.activeIdentity()
                ?: return PlayerResult.Failure(PlayerFailure.PlaybackUnavailable)
        return getForIdentity(
            identity = identity,
            trackId = trackId,
            forceRefresh = forceRefresh,
        )
    }

    private suspend fun getForIdentity(
        identity: ActiveSessionIdentity,
        trackId: String,
        forceRefresh: Boolean,
    ): PlayerResult<PlaybackGrant> {
        var resolved: PlayerResult<PlaybackGrant>? = null
        while (resolved == null && isCurrentIdentity(identity) && prepareStoreFor(identity)) {
            val generation = synchronized(storeLock) { storeGeneration }
            val key = runCatching {
                requestKey(identity, trackId)
            }.getOrNull()
            if (key == null) {
                resolved = PlayerResult.Failure(PlayerFailure.PlaybackUnavailable)
            } else {
                val cached = if (forceRefresh) {
                    null
                } else {
                    cachedGrantForRequest(key, clock.millis(), identity, generation)
                }
                resolved =
                    cached?.let { PlayerResult.Success(it) }
                        ?: grantMutex(key).withLock {
                            getLocked(
                                identity = identity,
                                trackId = trackId,
                                key = key,
                                expectedGeneration = generation,
                                forceRefresh = forceRefresh,
                            )
                        }
            }
        }
        return resolved ?: PlayerResult.Failure(PlayerFailure.PlaybackUnavailable)
    }

    private suspend fun getLocked(
        identity: ActiveSessionIdentity,
        trackId: String,
        key: PlaybackGrantKey,
        expectedGeneration: Long,
        forceRefresh: Boolean,
    ): PlayerResult<PlaybackGrant>? {
        val lockedNow = clock.millis()
        if (!forceRefresh) {
            cachedGrantForRequest(key, lockedNow, identity, expectedGeneration)
                ?.let { return PlayerResult.Success(it) }
        }
        if (!isRequestCurrent(identity, expectedGeneration)) return null
        return requestGrant(identity, trackId, key, expectedGeneration)
    }

    private suspend fun requestGrant(
        identity: ActiveSessionIdentity,
        trackId: String,
        key: PlaybackGrantKey,
        requestGeneration: Long,
    ): PlayerResult<PlaybackGrant> {
        return try {
            val response = api.grant(trackId, PlaybackRequestDto())
            if (!response.isSuccessful) {
                val error =
                    problemResponseParser.parse(
                        status = response.code(),
                        body = response.errorBody()?.string(),
                        traceId = response.headers()[TRACE_ID_HEADER],
                        retryAfterSeconds = response.headers()[RETRY_AFTER_HEADER]?.toLongOrNull(),
                    )
                return PlayerResult.Failure(PlayerFailure.Unexpected(error.detail))
            }
            val dto = response.body()
                ?: return PlayerResult.Failure(PlayerFailure.PlaybackUnavailable)
            response
                .headers()[DATE_HEADER]
                ?.toHttpDateEpochMillis()
                ?.let(clock::synchronize)
            val now = clock.millis()
            val grant = dto.toDomain(
                expectedTrackId = trackId,
                now = now,
                endpoint = serverConfigRepository.currentEndpoint()
                    ?: return PlayerResult.Failure(PlayerFailure.PlaybackUnavailable),
            )
            if (storeGrant(identity, key, grant, requestGeneration)) {
                PlayerResult.Success(grant)
            } else {
                PlayerResult.Failure(PlayerFailure.PlaybackUnavailable)
            }
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: Exception) {
            PlayerResult.Failure(PlayerFailure.PlaybackUnavailable)
        }
    }

    private fun storeGrant(
        identity: ActiveSessionIdentity,
        key: PlaybackGrantKey,
        grant: PlaybackGrant,
        requestGeneration: Long,
    ): Boolean = synchronized(storeLock) {
        if (storeGeneration != requestGeneration || !isCurrentIdentity(identity)) {
            false
        } else {
            store.put(key, grant)
            true
        }
    }

    override fun invalidate(trackId: String) {
        synchronized(storeLock) {
            storeGeneration += 1
            store.invalidateTrack(trackId)
        }
    }

    override fun clear() {
        synchronized(storeLock) {
            storeGeneration += 1
            storeIdentity = null
            store.clear()
        }
        grantRegistry.clear()
    }

    private fun prepareStoreFor(identity: ActiveSessionIdentity): Boolean {
        var clearRegistry = false
        val prepared = synchronized(storeLock) {
            if (!isCurrentIdentity(identity)) return false
            if (storeIdentity == identity) return true
            storeGeneration += 1
            storeIdentity = identity
            store.clear()
            clearRegistry = true
            true
        }
        if (clearRegistry) {
            grantRegistry.clear()
        }
        return prepared
    }

    private fun cachedGrantForRequest(
        key: PlaybackGrantKey,
        now: Long,
        identity: ActiveSessionIdentity,
        expectedGeneration: Long,
    ): PlaybackGrant? = synchronized(storeLock) {
        if (!isRequestCurrent(identity, expectedGeneration)) {
            null
        } else {
            store.get(key)?.takeIf {
                it.expiresAtEpochMillis - EXPIRY_SAFETY_MARGIN_MS > now
            }
        }
    }

    private fun isRequestCurrent(identity: ActiveSessionIdentity, expectedGeneration: Long): Boolean =
        synchronized(storeLock) {
            storeGeneration == expectedGeneration && isCurrentIdentity(identity)
        }

    private fun requestKey(
        identity: ActiveSessionIdentity,
        trackId: String,
    ): PlaybackGrantKey {
        UUID.fromString(trackId)
        return PlaybackGrantKey(
            ownerUserId = identity.userId,
            sessionId = identity.sessionId,
            serverGeneration = identity.serverGeneration.value,
            trackId = trackId,
        )
    }

    private fun isCurrentIdentity(expected: ActiveSessionIdentity): Boolean =
        sessionIdentityProvider.activeIdentity() == expected

    private fun grantMutex(key: PlaybackGrantKey): Mutex =
        grantMutexes[(key.hashCode() and Int.MAX_VALUE) % grantMutexes.size]

    private fun PlaybackGrantDto.toDomain(
        expectedTrackId: String,
        now: Long,
        endpoint: com.xymusic.app.domain.server.ServerEndpoint,
    ): PlaybackGrant {
        require(trackId == expectedTrackId)
        UUID.fromString(trackId)
        val configuredUri = URI(endpoint.displayValue + "/")
        val rawUri = URI(streamUrl)
        require(rawUri.rawFragment == null && rawUri.rawUserInfo == null)
        val resolvedUri = if (rawUri.isAbsolute) rawUri else configuredUri.resolve(rawUri)
        require(resolvedUri.scheme == endpoint.protocol.scheme)
        require(resolvedUri.host.equals(endpoint.host, ignoreCase = true))
        val resolvedPort = if (resolvedUri.port == -1) endpoint.protocol.defaultPort else resolvedUri.port
        require(resolvedPort == endpoint.port)
        require(resolvedUri.rawPath.startsWith("/"))
        val expiry = Instant.parse(expiresAt).toEpochMilli()
        require(Math.subtractExact(expiry, now) > MINIMUM_GRANT_LIFETIME_MS)
        require(bitrate > 0)
        require(sampleRate == null || sampleRate > 0)
        require(contentLength == null || contentLength > 0)
        return PlaybackGrant(
            trackId = trackId,
            streamUrl = resolvedUri.toString(),
            expiresAtEpochMillis = expiry,
            mimeType = mimeType,
            codec = codec,
            container = container,
            bitrate = bitrate,
            sampleRate = sampleRate,
            contentLength = contentLength,
            durationMs = durationMs?.also { require(it > 0) },
        )
    }

    private fun String.toHttpDateEpochMillis(): Long? = runCatching {
        ZonedDateTime
            .parse(this, DateTimeFormatter.RFC_1123_DATE_TIME)
            .toInstant()
            .toEpochMilli()
    }.getOrNull()

    private companion object {
        const val EXPIRY_SAFETY_MARGIN_MS = 30_000L
        const val MINIMUM_GRANT_LIFETIME_MS = 5_000L
        const val GRANT_MUTEX_COUNT = 32
        const val TRACE_ID_HEADER = "X-Trace-Id"
        const val RETRY_AFTER_HEADER = "Retry-After"
        const val DATE_HEADER = "Date"
    }
}
