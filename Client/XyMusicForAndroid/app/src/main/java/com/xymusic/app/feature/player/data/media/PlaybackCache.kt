package com.xymusic.app.feature.player.data.media

import android.content.Context
import androidx.media3.common.C
import androidx.media3.common.util.UnstableApi
import androidx.media3.database.StandaloneDatabaseProvider
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.cache.Cache
import androidx.media3.datasource.cache.CacheDataSource
import androidx.media3.datasource.cache.CacheEvictor
import androidx.media3.datasource.cache.CacheSpan
import androidx.media3.datasource.cache.ContentMetadata
import androidx.media3.datasource.cache.SimpleCache
import androidx.media3.datasource.okhttp.OkHttpDataSource
import com.xymusic.app.core.common.IoDispatcher
import com.xymusic.app.core.database.dao.OfflineTrackDao
import com.xymusic.app.core.network.MediaHttpClient
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.domain.settings.AppSettingsRepository
import dagger.hilt.android.qualifiers.ApplicationContext
import java.util.TreeSet
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient

@Singleton
@UnstableApi
class PlaybackCache
@Inject
constructor(
    @ApplicationContext private val context: Context,
    settingsRepository: AppSettingsRepository,
    offlineTrackDao: OfflineTrackDao,
    @IoDispatcher private val ioDispatcher: CoroutineDispatcher,
) : OfflineMediaCache {
    private val evictor = AdjustableLeastRecentlyUsedCacheEvictor()
    private val scope = CoroutineScope(SupervisorJob() + ioDispatcher)
    private val clearMutex = Mutex()
    private val cacheDelegate =
        lazy(LazyThreadSafetyMode.SYNCHRONIZED) {
            SimpleCache(
                context.filesDir.resolve("playback-media"),
                evictor,
                StandaloneDatabaseProvider(context),
            ).also { created -> evictor.onCacheCreated(created) }
        }

    override val cache: Cache
        get() = cacheDelegate.value

    init {
        scope.launch {
            combine(
                settingsRepository.settings.map { settings ->
                    settings.cacheLimitMiB.toLong() * BYTES_PER_MIB
                },
                offlineTrackDao.observeCacheKeys().map { keys -> keys.toSet() },
            ) { maxBytes, persistentPins -> CachePolicy(maxBytes, persistentPins) }
                .distinctUntilChanged()
                .collect { policy ->
                    // Records the policy without touching the cache. Creating SimpleCache
                    // scans the cache directory and opens its own database, so it must not
                    // happen during PlaybackService.onCreate; onCacheCreated applies the
                    // pending policy when playback first needs the cache.
                    evictor.updatePolicy(policy.maxBytes, policy.persistentPins)
                }
        }
    }

    override fun pin(cacheKey: String) {
        evictor.pin(cacheKey)
    }

    override fun unpin(cacheKey: String) {
        evictor.unpin(cacheKey)
    }

    override fun promotePin(cacheKey: String) {
        evictor.promotePin(cacheKey)
    }

    override fun isFullyCached(cacheKey: String, contentLength: Long): Boolean =
        contentLength > 0 && cache.isCached(cacheKey, 0, contentLength)

    override fun cachedContentLength(cacheKey: String): Long? {
        val length = cache.getContentMetadata(cacheKey).get(ContentMetadata.KEY_CONTENT_LENGTH, C.LENGTH_UNSET.toLong())
        return length.takeIf { it > 0 }
    }

    override suspend fun remove(cacheKey: String) = withContext(ioDispatcher) {
        clearMutex.withLock {
            try {
                cache.removeResource(cacheKey)
            } finally {
                evictor.removeAllPins(cacheKey)
            }
        }
    }

    override suspend fun clear() = withContext(ioDispatcher) {
        clearMutex.withLock {
            evictor.clearPins()
            cache.keys.toList().forEach { key ->
                cache.removeResource(key)
                if (key in cache.keys) {
                    val holeSpan =
                        checkNotNull(cache.startReadWriteNonBlocking(key, 0, 1)) {
                            "Playback cache resource is still in use: $key"
                        }
                    check(holeSpan.isHoleSpan) {
                        "Playback cache resource could not be fully removed: $key"
                    }
                    cache.releaseHoleSpan(holeSpan)
                }
            }
            check(cache.keys.isEmpty()) { "Playback cache was not fully cleared" }
        }
    }

    private companion object {
        const val BYTES_PER_MIB = 1024L * 1024L
    }

    private data class CachePolicy(val maxBytes: Long, val persistentPins: Set<String>)
}

@UnstableApi
internal class AdjustableLeastRecentlyUsedCacheEvictor : CacheEvictor {
    private val leastRecentlyUsed =
        TreeSet(
            compareBy<CacheSpan> { it.lastTouchTimestamp }
                .thenBy { it.key }
                .thenBy { it.position },
        )
    private var currentSizeBytes = 0L
    private var maxBytes = Long.MAX_VALUE
    private var evictionEnabled = false
    private var pendingMaxBytes: Long? = null
    private var pendingPersistentPins: Set<String> = emptySet()
    private val persistentPinnedKeys = mutableSetOf<String>()
    private val optimisticPersistentPinnedKeys = mutableSetOf<String>()
    private val temporaryPinnedKeys = mutableSetOf<String>()

    /**
     * Records the desired policy without touching the cache, so the cache can stay
     * uncreated until playback needs it. Applied by [onCacheCreated].
     */
    @Synchronized
    fun updatePolicy(maxBytes: Long, persistentPins: Set<String>) {
        require(maxBytes > 0) { "Cache limit must be positive" }
        pendingMaxBytes = maxBytes
        pendingPersistentPins = persistentPins
        persistentPinnedKeys.clear()
        persistentPinnedKeys.addAll(persistentPins)
        optimisticPersistentPinnedKeys.removeAll(persistentPins)
        evictionEnabled = true
    }

    /** Applies the policy recorded before the cache existed and evicts down to it. */
    fun onCacheCreated(cache: Cache) {
        evict(cache, 0)
    }

    @Synchronized
    fun clearPins() {
        persistentPinnedKeys.clear()
        optimisticPersistentPinnedKeys.clear()
        temporaryPinnedKeys.clear()
    }

    @Synchronized
    fun pin(key: String) {
        temporaryPinnedKeys += key
    }

    @Synchronized
    fun unpin(key: String) {
        temporaryPinnedKeys -= key
    }

    @Synchronized
    fun promotePin(key: String) {
        if (key !in persistentPinnedKeys) optimisticPersistentPinnedKeys += key
    }

    @Synchronized
    fun removeAllPins(key: String) {
        persistentPinnedKeys -= key
        optimisticPersistentPinnedKeys -= key
        temporaryPinnedKeys -= key
    }

    override fun requiresCacheSpanTouches(): Boolean = true

    override fun onCacheInitialized() = Unit

    override fun onStartFile(cache: Cache, key: String, position: Long, length: Long) {
        evict(cache, length.coerceAtLeast(0))
    }

    override fun onSpanAdded(cache: Cache, span: CacheSpan) {
        synchronized(this) {
            leastRecentlyUsed.add(span)
            currentSizeBytes += span.length
        }
        evict(cache, 0)
    }

    @Synchronized
    override fun onSpanRemoved(cache: Cache, span: CacheSpan) {
        if (leastRecentlyUsed.remove(span)) currentSizeBytes -= span.length
    }

    override fun onSpanTouched(cache: Cache, oldSpan: CacheSpan, newSpan: CacheSpan) {
        synchronized(this) {
            if (leastRecentlyUsed.remove(oldSpan)) currentSizeBytes -= oldSpan.length
            leastRecentlyUsed.add(newSpan)
            currentSizeBytes += newSpan.length
        }
        evict(cache, 0)
    }

    private fun evict(cache: Cache, incomingLength: Long) {
        while (true) {
            val candidate =
                synchronized(this) {
                    // A policy recorded before the cache existed takes effect here, on the
                    // first eviction after creation, without forcing the cache into being.
                    pendingMaxBytes?.let { pending ->
                        maxBytes = pending
                        pendingMaxBytes = null
                    }
                    if (
                        !evictionEnabled ||
                        currentSizeBytes + incomingLength <= maxBytes ||
                        leastRecentlyUsed.isEmpty()
                    ) {
                        null
                    } else {
                        leastRecentlyUsed.firstOrNull { span -> !isPinned(span.key) }
                    }
                } ?: return
            cache.removeSpan(candidate)
        }
    }

    private fun isPinned(key: String): Boolean = key in persistentPinnedKeys ||
        key in optimisticPersistentPinnedKeys ||
        key in temporaryPinnedKeys
}

@UnstableApi
fun playbackDataSourceFactory(
    @MediaHttpClient mediaHttpClient: OkHttpClient,
    offlineMediaCache: OfflineMediaCache,
    grantRegistry: PlaybackGrantRegistry,
    networkPolicy: PlaybackNetworkPolicy,
    sessionIdentityProvider: SessionIdentityProvider,
): DataSource.Factory {
    val networkFactory =
        PolicyEnforcingDataSourceFactory(
            OkHttpDataSource.Factory(mediaHttpClient),
            networkPolicy,
        )
    val onlineFactory =
        deferredCacheDataSourceFactory(
            cacheProvider = { offlineMediaCache.cache },
            upstreamFactory = networkFactory,
        )
    val offlineFactory =
        deferredReadOnlyCacheDataSourceFactory(
            cacheProvider = { offlineMediaCache.cache },
        )
    return GrantResolvingDataSourceFactory(
        onlineFactory = onlineFactory,
        networkFactory = networkFactory,
        offlineFactory = offlineFactory,
        grantRegistry = grantRegistry,
        sessionIdentityProvider = sessionIdentityProvider,
    )
}

@UnstableApi
internal fun deferredCacheDataSourceFactory(
    cacheProvider: () -> Cache,
    upstreamFactory: DataSource.Factory,
): DataSource.Factory = DataSource.Factory {
    CacheDataSource
        .Factory()
        .setCache(cacheProvider())
        .setUpstreamDataSourceFactory(upstreamFactory)
        .setFlags(CacheDataSource.FLAG_IGNORE_CACHE_ON_ERROR)
        .createDataSource()
}

@UnstableApi
internal fun deferredReadOnlyCacheDataSourceFactory(cacheProvider: () -> Cache): DataSource.Factory =
    DataSource.Factory {
        CacheDataSource
            .Factory()
            .setCache(cacheProvider())
            .setCacheWriteDataSinkFactory(null)
            .createDataSource()
    }
