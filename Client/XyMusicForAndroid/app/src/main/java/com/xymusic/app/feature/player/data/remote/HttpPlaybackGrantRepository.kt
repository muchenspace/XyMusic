package com.xymusic.app.feature.player.data.remote

import com.xymusic.app.core.network.ServerConfigRepository
import com.xymusic.app.core.network.ServerProtocol
import com.xymusic.app.core.network.ServerSynchronizedClock
import com.xymusic.app.core.preferences.AppSettingsRepository
import com.xymusic.app.core.preferences.StreamingQuality
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.data.network.ProblemResponseParser
import com.xymusic.app.feature.player.data.media.PlaybackGrantKey
import com.xymusic.app.feature.player.data.media.PlaybackGrantStore
import com.xymusic.app.feature.player.domain.PlaybackGrant
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.model.PlayerFailure
import com.xymusic.app.feature.player.domain.model.PreferredQuality
import java.net.URI
import java.time.Instant
import java.time.ZonedDateTime
import java.time.format.DateTimeFormatter
import java.util.UUID
import java.util.concurrent.CancellationException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

@Singleton
class HttpPlaybackGrantRepository
@Inject
constructor(
    private val api: PlaybackApi,
    private val store: PlaybackGrantStore,
    private val problemResponseParser: ProblemResponseParser,
    private val settingsRepository: AppSettingsRepository,
    private val serverConfigRepository: ServerConfigRepository,
    private val sessionIdentityProvider: SessionIdentityProvider,
    private val clock: ServerSynchronizedClock,
) : PlaybackGrantRepository {
    private val grantMutexes = Array(GRANT_MUTEX_COUNT) { Mutex() }
    private val storeLock = Any()
    private val compatibleCodecFallbackTrackIds = mutableSetOf<String>()
    private var storeGeneration = 0L
    private var storeIdentity: ActiveSessionIdentity? = null

    override suspend fun get(
        trackId: String,
        preferredQuality: PreferredQuality,
        acceptedCodecs: List<String>,
        forceRefresh: Boolean,
    ): PlayerResult<PlaybackGrant> {
        val identity =
            sessionIdentityProvider.activeIdentity()
                ?: return PlayerResult.Failure(PlayerFailure.PlaybackUnavailable)
        return getForIdentity(identity, trackId, preferredQuality, acceptedCodecs, forceRefresh)
    }

    private suspend fun getForIdentity(
        identity: ActiveSessionIdentity,
        trackId: String,
        preferredQuality: PreferredQuality,
        acceptedCodecs: List<String>,
        forceRefresh: Boolean,
    ): PlayerResult<PlaybackGrant> {
        val effectiveQuality = resolveEffectiveQuality(preferredQuality)
        var resolved: PlayerResult<PlaybackGrant>? = null
        while (resolved == null && isCurrentIdentity(identity) && prepareStoreFor(identity)) {
            val policy = requestPolicy(trackId, acceptedCodecs)
            val key =
                runCatching {
                    requestKey(identity, trackId, effectiveQuality, policy.acceptedCodecs)
                }.getOrNull()
            if (key == null) {
                resolved = PlayerResult.Failure(PlayerFailure.PlaybackUnavailable)
            } else {
                val cached =
                    if (forceRefresh) {
                        null
                    } else {
                        cachedGrantForRequest(key, clock.millis(), identity, policy.generation)
                    }
                resolved =
                    cached?.let { PlayerResult.Success(it) }
                        ?: grantMutex(key).withLock {
                            getLocked(
                                identity = identity,
                                trackId = trackId,
                                effectiveQuality = effectiveQuality,
                                key = key,
                                expectedGeneration = policy.generation,
                                forceRefresh = forceRefresh,
                            )
                        }
            }
        }
        return resolved ?: PlayerResult.Failure(PlayerFailure.PlaybackUnavailable)
    }

    private suspend fun resolveEffectiveQuality(preferredQuality: PreferredQuality): PreferredQuality = try {
        settingsRepository.settings
            .first()
            .streamingQuality
            .toPreferredQuality()
    } catch (failure: CancellationException) {
        throw failure
    } catch (_: Exception) {
        preferredQuality
    }

    private suspend fun getLocked(
        identity: ActiveSessionIdentity,
        trackId: String,
        effectiveQuality: PreferredQuality,
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
        return requestGrant(identity, trackId, effectiveQuality, key, expectedGeneration)
    }

    private suspend fun requestGrant(
        identity: ActiveSessionIdentity,
        trackId: String,
        effectiveQuality: PreferredQuality,
        key: PlaybackGrantKey,
        requestGeneration: Long,
    ): PlayerResult<PlaybackGrant> {
        return try {
            val response =
                api.grant(
                    trackId,
                    PlaybackRequestDto(effectiveQuality.name, key.acceptedCodecs),
                )
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
            val dto =
                response.body()
                    ?: return PlayerResult.Failure(PlayerFailure.PlaybackUnavailable)
            response
                .headers()[DATE_HEADER]
                ?.toHttpDateEpochMillis()
                ?.let(clock::synchronize)
            val now = clock.millis()
            val grant =
                dto.toDomain(
                    expectedTrackId = trackId,
                    now = now,
                    allowCleartext =
                    serverConfigRepository.currentEndpoint()?.protocol ==
                        ServerProtocol.HTTP,
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

    override fun enableCompatibleCodecFallback(trackId: String): Boolean = synchronized(storeLock) {
        if (!compatibleCodecFallbackTrackIds.add(trackId)) {
            false
        } else {
            storeGeneration += 1
            store.invalidateTrack(trackId)
            true
        }
    }

    override fun isCompatibleCodecFallbackEnabled(trackId: String): Boolean = synchronized(storeLock) {
        trackId in compatibleCodecFallbackTrackIds
    }

    override fun clear() {
        synchronized(storeLock) {
            storeGeneration += 1
            storeIdentity = null
            compatibleCodecFallbackTrackIds.clear()
            store.clear()
        }
    }

    private fun prepareStoreFor(identity: ActiveSessionIdentity): Boolean {
        synchronized(storeLock) {
            if (!isCurrentIdentity(identity)) return false
            if (storeIdentity == identity) return true
            val previousIdentity = storeIdentity
            storeGeneration += 1
            storeIdentity = identity
            if (previousIdentity != null) compatibleCodecFallbackTrackIds.clear()
            store.clear()
            return true
        }
    }

    private fun requestPolicy(trackId: String, requestedCodecs: List<String>): PlaybackGrantRequestPolicy =
        synchronized(storeLock) {
            PlaybackGrantRequestPolicy(
                acceptedCodecs =
                if (trackId in compatibleCodecFallbackTrackIds) {
                    COMPATIBLE_AUDIO_CODECS
                } else {
                    requestedCodecs
                },
                generation = storeGeneration,
            )
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
        preferredQuality: PreferredQuality,
        acceptedCodecs: List<String>,
    ): PlaybackGrantKey {
        UUID.fromString(trackId)
        require(acceptedCodecs.size <= 10)
        require(acceptedCodecs.distinct().size == acceptedCodecs.size)
        require(acceptedCodecs.all { it.isNotBlank() && it.length <= 30 })
        return PlaybackGrantKey(
            ownerUserId = identity.userId,
            sessionId = identity.sessionId,
            serverGeneration = identity.serverGeneration.value,
            trackId = trackId,
            preferredQuality = preferredQuality,
            acceptedCodecs = acceptedCodecs.sorted(),
        )
    }

    private fun isCurrentIdentity(expected: ActiveSessionIdentity): Boolean =
        sessionIdentityProvider.activeIdentity() == expected

    private fun StreamingQuality.toPreferredQuality(): PreferredQuality = PreferredQuality.valueOf(name)

    private fun grantMutex(key: PlaybackGrantKey): Mutex =
        grantMutexes[(key.hashCode() and Int.MAX_VALUE) % grantMutexes.size]

    private fun PlaybackGrantDto.toDomain(expectedTrackId: String, now: Long, allowCleartext: Boolean): PlaybackGrant {
        require(trackId == expectedTrackId)
        UUID.fromString(trackId)
        UUID.fromString(variantId)
        val signedUri = URI(url)
        require(signedUri.scheme == "https" || allowCleartext && signedUri.scheme == "http")
        require(!signedUri.host.isNullOrBlank() && signedUri.rawUserInfo == null)
        require(signedUri.rawFragment == null)
        val expiry = Instant.parse(expiresAt).toEpochMilli()
        require(Math.subtractExact(expiry, now) > MINIMUM_GRANT_LIFETIME_MS)
        require(bitrate > 0 && contentLength > 0)
        require(sampleRate == null || sampleRate > 0)
        require(cacheKey.isNotBlank() && !cacheKey.contains("?"))
        require(checksumSha256 == null || CHECKSUM_REGEX.matches(checksumSha256))
        return PlaybackGrant(
            trackId = trackId,
            variantId = variantId,
            selectedQuality = PreferredQuality.valueOf(selectedQuality),
            signedUrl = url,
            expiresAtEpochMillis = expiry,
            mimeType = mimeType,
            codec = codec,
            container = container,
            bitrate = bitrate,
            sampleRate = sampleRate,
            contentLength = contentLength,
            checksumSha256 = checksumSha256,
            cacheKey = cacheKey,
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
        val COMPATIBLE_AUDIO_CODECS = listOf("aac", "mp3", "opus")
        val CHECKSUM_REGEX = Regex("^[a-f0-9]{64}$")
    }
}

private data class PlaybackGrantRequestPolicy(val acceptedCodecs: List<String>, val generation: Long)
