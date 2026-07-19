package com.xymusic.app.feature.player.data.media

import android.app.Application
import android.content.Context
import android.content.ContextWrapper
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.cache.Cache
import androidx.media3.datasource.cache.CacheSpan
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.database.dao.OfflineTrackDao
import com.xymusic.app.core.database.entity.OfflineTrackEntity
import com.xymusic.app.core.preferences.AppSettings
import com.xymusic.app.core.preferences.AppSettingsRepository
import java.io.File
import java.lang.reflect.Proxy
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.emptyFlow
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@UnstableApi
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class PlaybackCacheTest {
    @Test
    fun constructionDoesNotReadTheCacheDirectory() {
        val context =
            object : ContextWrapper(
                ApplicationProvider.getApplicationContext<Context>(),
            ) {
                var filesDirReadCount = 0

                override fun getFilesDir(): File {
                    filesDirReadCount += 1
                    error("Cache directory must be resolved lazily")
                }
            }

        PlaybackCache(
            context = context,
            settingsRepository = EmptySettingsRepository,
            offlineTrackDao = EmptyOfflineTrackDao,
            ioDispatcher = Dispatchers.Unconfined,
        )

        assertThat(context.filesDirReadCount).isEqualTo(0)
    }

    @Test
    fun cacheDataSourceFactoryDefersCacheLookupUntilSourceCreation() {
        var cacheLookupCount = 0
        val factory =
            deferredCacheDataSourceFactory(
                cacheProvider = {
                    cacheLookupCount += 1
                    throw ExpectedCacheLookup()
                },
                upstreamFactory = DataSource.Factory { error("Upstream source must not be created") },
            )

        assertThat(cacheLookupCount).isEqualTo(0)

        val failure = runCatching { factory.createDataSource() }.exceptionOrNull()

        assertThat(failure).isInstanceOf(ExpectedCacheLookup::class.java)
        assertThat(cacheLookupCount).isEqualTo(1)
    }

    @Test
    fun loweringCacheLimitImmediatelyEvictsLeastRecentlyUsedUnpinnedSpan() {
        val evictor = AdjustableLeastRecentlyUsedCacheEvictor()
        val cache = RecordingCache(evictor)
        cache.add(cachedSpan(key = "old", length = 60, lastTouchTimestamp = 1))
        cache.add(cachedSpan(key = "new", length = 60, lastTouchTimestamp = 2))

        evictor.updatePolicy(cache.delegate, maxBytes = 200, persistentPins = emptySet())
        assertThat(cache.removedKeys).isEmpty()

        evictor.updatePolicy(cache.delegate, maxBytes = 100, persistentPins = emptySet())

        assertThat(cache.removedKeys).containsExactly("old")
        assertThat(cache.activeKeys).containsExactly("new")
    }

    @Test
    fun cacheLimitUpdateKeepsDownloadedMediaPinnedAndEvictsAnotherSpan() {
        val evictor = AdjustableLeastRecentlyUsedCacheEvictor()
        val cache = RecordingCache(evictor)
        cache.add(cachedSpan(key = "downloaded", length = 80, lastTouchTimestamp = 1))
        cache.add(cachedSpan(key = "streamed", length = 40, lastTouchTimestamp = 2))

        evictor.updatePolicy(
            cache.delegate,
            maxBytes = 80,
            persistentPins = setOf("downloaded"),
        )

        assertThat(cache.removedKeys).containsExactly("streamed")
        assertThat(cache.activeKeys).containsExactly("downloaded")
    }

    private object EmptySettingsRepository : AppSettingsRepository {
        override val settings: Flow<AppSettings> = emptyFlow()

        override suspend fun update(settings: AppSettings) = Unit

        override suspend fun mutate(transform: (AppSettings) -> AppSettings) = Unit

        override suspend fun reset() = Unit
    }

    private object EmptyOfflineTrackDao : OfflineTrackDao {
        override fun observeAll(ownerUserId: String): Flow<List<OfflineTrackEntity>> = emptyFlow()

        override fun observeDownloaded(ownerUserId: String, trackId: String): Flow<Boolean> = emptyFlow()

        override suspend fun track(ownerUserId: String, trackId: String): OfflineTrackEntity? = null

        override suspend fun tracks(ownerUserId: String): List<OfflineTrackEntity> = emptyList()

        override fun observeCacheKeys(): Flow<List<String>> = emptyFlow()

        override suspend fun cacheKeyReferenceCount(cacheKey: String): Int = 0

        override suspend fun upsert(track: OfflineTrackEntity) = Unit

        override suspend fun delete(ownerUserId: String, trackId: String): Int = 0

        override suspend fun deleteOwner(ownerUserId: String): Int = 0

        override suspend fun clear(): Int = 0
    }

    private class ExpectedCacheLookup : RuntimeException()

    private class RecordingCache(private val evictor: AdjustableLeastRecentlyUsedCacheEvictor) {
        private val spans = linkedSetOf<CacheSpan>()
        val removedKeys = mutableListOf<String>()
        val activeKeys: Set<String>
            get() = spans.mapTo(linkedSetOf(), CacheSpan::key)

        val delegate: Cache =
            Proxy.newProxyInstance(
                Cache::class.java.classLoader,
                arrayOf(Cache::class.java),
            ) { _, method, args ->
                when (method.name) {
                    "removeSpan" -> {
                        val span = args!![0] as CacheSpan
                        if (spans.remove(span)) {
                            removedKeys += span.key
                            evictor.onSpanRemoved(delegate, span)
                        }
                        Unit
                    }

                    "getCacheSpace" -> spans.sumOf(CacheSpan::length)
                    "getKeys" -> activeKeys
                    "getCachedSpans" -> java.util.TreeSet(spans)
                    "getUid" -> 1L
                    "isCached" -> false
                    "getCachedLength", "getCachedBytes" -> 0L
                    "toString" -> "RecordingCache"
                    "hashCode" -> System.identityHashCode(this)
                    "equals" -> args?.firstOrNull() === this
                    else -> defaultValue(method.returnType)
                }
            } as Cache

        fun add(span: CacheSpan) {
            spans += span
            evictor.onSpanAdded(delegate, span)
        }
    }

    private companion object {
        fun cachedSpan(key: String, length: Long, lastTouchTimestamp: Long): CacheSpan =
            CacheSpan(key, 0, length, lastTouchTimestamp, File("$key.cache"))

        fun defaultValue(type: Class<*>): Any? = when (type) {
            java.lang.Boolean.TYPE -> false
            java.lang.Byte.TYPE -> 0.toByte()
            java.lang.Short.TYPE -> 0.toShort()
            java.lang.Integer.TYPE -> 0
            java.lang.Long.TYPE -> 0L
            java.lang.Float.TYPE -> 0f
            java.lang.Double.TYPE -> 0.0
            java.lang.Character.TYPE -> '\u0000'
            else -> null
        }
    }
}
