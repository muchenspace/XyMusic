package com.xymusic.app.feature.player.data.remote

import com.xymusic.app.core.network.ServerSynchronizedClock
import com.xymusic.app.core.session.ActiveSessionIdentity
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.data.network.ProblemResponseParser
import com.xymusic.app.domain.server.ServerConfigRepository
import com.xymusic.app.domain.settings.AppSettingsRepository
import com.xymusic.app.feature.player.data.media.PlaybackGrantKey
import com.xymusic.app.feature.player.data.media.PlaybackGrantRegistry
import com.xymusic.app.feature.player.data.media.PlaybackGrantStore
import com.xymusic.app.feature.player.domain.AutomaticPlaybackQualityPolicy
import com.xymusic.app.feature.player.domain.PlaybackGrant
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.PlaybackStreamProtocol
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
    private val automaticQualityController: AutomaticPlaybackQualityPolicy,
    private val grantRegistry: PlaybackGrantRegistry,
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
        streamProtocol: PlaybackStreamProtocol?,
        startPositionMs: Long,
    ): PlayerResult<PlaybackGrant> {
        val identity =
            sessionIdentityProvider.activeIdentity()
                ?: return PlayerResult.Failure(PlayerFailure.PlaybackUnavailable)
        return getForIdentity(
            identity,
            trackId,
            preferredQuality,
            acceptedCodecs,
            forceRefresh,
            streamProtocol,
            startPositionMs,
        )
    }

    private suspend fun getForIdentity(
        identity: ActiveSessionIdentity,
        trackId: String,
        preferredQuality: PreferredQuality,
        acceptedCodecs: List<String>,
        forceRefresh: Boolean,
        requestedStreamProtocol: PlaybackStreamProtocol?,
        startPositionMs: Long,
    ): PlayerResult<PlaybackGrant> {
        if (startPositionMs < 0 || (startPositionMs > 0 && requestedStreamProtocol != PlaybackStreamProtocol.HLS)) {
            return PlayerResult.Failure(PlayerFailure.PlaybackUnavailable)
        }
        var resolved: PlayerResult<PlaybackGrant>? = null
        var effectiveQuality: PreferredQuality? = null
        while (resolved == null && isCurrentIdentity(identity) && prepareStoreFor(identity)) {
            val quality = effectiveQuality ?: resolveEffectiveQuality(trackId, preferredQuality).also {
                effectiveQuality = it
            }
            val protocol = requestedStreamProtocol ?: protocolForQuality(quality)
            val policy = requestPolicy(trackId, acceptedCodecs, protocol)
            val key =
                runCatching {
                    requestKey(identity, trackId, quality, policy.acceptedCodecs, protocol)
                }.getOrNull()
            if (key == null) {
                resolved = PlayerResult.Failure(PlayerFailure.PlaybackUnavailable)
            } else {
                val cached =
                    if (forceRefresh || startPositionMs > 0) {
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
                                effectiveQuality = quality,
                                key = key,
                                expectedGeneration = policy.generation,
                                forceRefresh = forceRefresh,
                                startPositionMs = startPositionMs,
                            )
                        }
            }
        }
        return resolved ?: PlayerResult.Failure(PlayerFailure.PlaybackUnavailable)
    }

    private suspend fun resolveEffectiveQuality(
        trackId: String,
        preferredQuality: PreferredQuality,
    ): PreferredQuality = try {
        automaticQualityController.resolveTrackQuality(
            trackId,
            settingsRepository.settings.first().streamingQuality,
        )
    } catch (failure: CancellationException) {
        throw failure
    } catch (_: Exception) {
        automaticQualityController.lockConcreteQuality(trackId, preferredQuality)
    }

    private suspend fun getLocked(
        identity: ActiveSessionIdentity,
        trackId: String,
        effectiveQuality: PreferredQuality,
        key: PlaybackGrantKey,
        expectedGeneration: Long,
        forceRefresh: Boolean,
        startPositionMs: Long,
    ): PlayerResult<PlaybackGrant>? {
        val lockedNow = clock.millis()
        if (!forceRefresh && startPositionMs == 0L) {
            cachedGrantForRequest(key, lockedNow, identity, expectedGeneration)
                ?.let { return PlayerResult.Success(it) }
        }
        if (!isRequestCurrent(identity, expectedGeneration)) return null
        return requestGrant(identity, trackId, effectiveQuality, key, expectedGeneration, startPositionMs)
    }

    private suspend fun requestGrant(
        identity: ActiveSessionIdentity,
        trackId: String,
        effectiveQuality: PreferredQuality,
        key: PlaybackGrantKey,
        requestGeneration: Long,
        startPositionMs: Long,
    ): PlayerResult<PlaybackGrant> {
        return try {
            val response =
                api.grant(
                    trackId,
                    PlaybackRequestDto(
                        preferredQuality = effectiveQuality.name,
                        acceptedCodecs = key.acceptedCodecs,
                        streamProtocol = key.streamProtocol.name,
                        startPositionMs = startPositionMs.takeIf {
                            key.streamProtocol == PlaybackStreamProtocol.HLS && it > 0
                        },
                    ),
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
                    endpoint = serverConfigRepository.currentEndpoint()
                        ?: return PlayerResult.Failure(PlayerFailure.PlaybackUnavailable),
                    requestedStartPositionMs = startPositionMs,
                )
            automaticQualityController.recordSelectedQuality(trackId, grant.selectedQuality)
            if (startPositionMs > 0) {
                if (isRequestCurrent(identity, requestGeneration)) {
                    PlayerResult.Success(grant)
                } else {
                    PlayerResult.Failure(PlayerFailure.PlaybackUnavailable)
                }
            } else if (storeGrant(identity, key, grant, requestGeneration)) {
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
        grantRegistry.clear()
        automaticQualityController.resetSession()
    }

    private fun prepareStoreFor(identity: ActiveSessionIdentity): Boolean {
        var resetAutomaticQuality = false
        val prepared = synchronized(storeLock) {
            if (!isCurrentIdentity(identity)) return false
            if (storeIdentity == identity) return true
            val previousIdentity = storeIdentity
            storeGeneration += 1
            storeIdentity = identity
            if (previousIdentity != null) compatibleCodecFallbackTrackIds.clear()
            store.clear()
            resetAutomaticQuality = true
            true
        }
        if (resetAutomaticQuality) {
            grantRegistry.clear()
            automaticQualityController.resetSession()
        }
        return prepared
    }

    private fun requestPolicy(
        trackId: String,
        requestedCodecs: List<String>,
        streamProtocol: PlaybackStreamProtocol,
    ): PlaybackGrantRequestPolicy =
        synchronized(storeLock) {
            PlaybackGrantRequestPolicy(
                acceptedCodecs =
                if (trackId in compatibleCodecFallbackTrackIds) {
                    COMPATIBLE_AUDIO_CODECS
                } else {
                    requestedCodecs
                },
                streamProtocol = streamProtocol,
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
        streamProtocol: PlaybackStreamProtocol,
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
            streamProtocol = streamProtocol,
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
        requestedStartPositionMs: Long,
    ): PlaybackGrant {
        require(trackId == expectedTrackId)
        UUID.fromString(trackId)
        UUID.fromString(sessionId)
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
        require(cacheKey.isNotBlank() && !cacheKey.contains("?"))
        require(checksumSha256 == null || CHECKSUM_REGEX.matches(checksumSha256))
        val resolvedProtocol = streamProtocol.toPlaybackStreamProtocol(resolvedUri)
        require(startPositionMs >= 0)
        if (requestedStartPositionMs > 0) {
            require(resolvedProtocol == PlaybackStreamProtocol.HLS)
            require(startPositionMs == 0L || startPositionMs == requestedStartPositionMs)
        }
        val effectiveStartPositionMs =
            if (requestedStartPositionMs > 0) requestedStartPositionMs else startPositionMs
        return PlaybackGrant(
            trackId = trackId,
            sessionId = sessionId,
            selectedQuality = PreferredQuality.valueOf(selectedQuality),
            streamUrl = resolvedUri.toString(),
            expiresAtEpochMillis = expiry,
            mimeType = mimeType,
            codec = codec,
            container = container,
            bitrate = bitrate,
            sampleRate = sampleRate,
            contentLength = contentLength,
            checksumSha256 = checksumSha256,
            cacheKey = cacheKey,
            streamProtocol = resolvedProtocol,
            durationMs = durationMs?.also { require(it > 0) },
            startPositionMs = effectiveStartPositionMs,
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

private fun protocolForQuality(quality: PreferredQuality): PlaybackStreamProtocol =
    PlaybackStreamProtocol.HLS

private fun String?.toPlaybackStreamProtocol(uri: URI): PlaybackStreamProtocol {
    val value = this?.trim()?.uppercase()
    if (!value.isNullOrEmpty()) {
        return runCatching { PlaybackStreamProtocol.valueOf(value) }.getOrElse {
            throw IllegalArgumentException("Invalid playback stream protocol")
        }
    }
    return if (uri.rawPath?.trimEnd('/')?.endsWith("/index.m3u8", ignoreCase = true) == true) {
        PlaybackStreamProtocol.HLS
    } else {
        PlaybackStreamProtocol.PROGRESSIVE
    }
}

private data class PlaybackGrantRequestPolicy(
    val acceptedCodecs: List<String>,
    val streamProtocol: PlaybackStreamProtocol,
    val generation: Long,
)
