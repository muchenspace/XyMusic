package com.xymusic.app.core.ui.component

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Color as AndroidColor
import androidx.compose.ui.graphics.Color
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.cache.Cache
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import coil3.asImage
import coil3.decode.DataSource
import coil3.request.ImageRequest
import coil3.request.SuccessResult
import coil3.request.allowHardware
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.app.ServerCacheCleaner
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionInvalidator
import com.xymusic.app.feature.player.data.media.OfflineMediaCache
import com.xymusic.app.feature.player.domain.PlaybackGrant
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import com.xymusic.app.feature.player.domain.PlayerEvent
import com.xymusic.app.feature.player.domain.PlayerRepository
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
import com.xymusic.app.feature.player.domain.model.PlayerState
import com.xymusic.app.feature.player.domain.model.RepeatMode
import com.xymusic.app.feature.search.data.SearchOverviewStore
import com.xymusic.app.feature.search.data.remote.SearchOverviewRemote
import com.xymusic.app.feature.search.domain.model.SearchOverview
import com.xymusic.app.feature.search.domain.model.SearchQuery
import com.xymusic.app.feature.settings.data.ProfileMemoryCache
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.test.runTest
import okhttp3.OkHttpClient
import org.junit.After
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
@UnstableApi
class AmbientColorExtractorTest {
    private val context: Application = ApplicationProvider.getApplicationContext()

    @Before
    fun setUp() {
        clearArtworkAmbientPaletteCache()
    }

    @After
    fun tearDown() {
        clearArtworkAmbientPaletteCache()
    }

    @Test
    fun extractArtworkAmbientColorDisablesHardwareBitmapsAndCachesPalette() = runTest {
        val cacheKey = "artwork:track-1:v1"
        lateinit var capturedRequest: ImageRequest
        val softwareBitmap =
            Bitmap.createBitmap(16, 16, Bitmap.Config.ARGB_8888).apply {
                eraseColor(AndroidColor.rgb(0x22, 0x66, 0xAA))
            }

        val palette =
            extractArtworkAmbientColor(
                artworkUrl = "https://media.example.test/cover.jpg",
                cacheKey = cacheKey,
                artworkIdentity = cacheKey,
                context = context,
                resources = context.resources,
                executeImageRequest = { request ->
                    capturedRequest = request
                    SuccessResult(
                        image = softwareBitmap.asImage(),
                        request = request,
                        dataSource = DataSource.NETWORK,
                    )
                },
            )

        assertThat(capturedRequest.allowHardware).isFalse()
        assertThat(capturedRequest.memoryCacheKey).isEqualTo("$cacheKey:ambient-128")
        assertThat(capturedRequest.diskCacheKey).isEqualTo(cacheKey)
        assertThat(palette).isNotNull()
        assertThat(cachedArtworkAmbientPaletteForTest(cacheKey)).isEqualTo(palette)
    }

    @Test
    fun serverCacheCleanerEvictsAmbientPaletteCache() = runTest {
        val cacheKey = "artwork:server-old:v1"
        val cachedPalette =
            ArtworkAmbientPalette(
                primary = Color(0xFF112233),
                secondary = Color(0xFF223344),
                highlight = Color(0xFF334455),
            )
        cacheArtworkAmbientPaletteForTest(cacheKey, cachedPalette)
        assertThat(cachedArtworkAmbientPaletteForTest(cacheKey)).isEqualTo(cachedPalette)

        val database =
            Room
                .inMemoryDatabaseBuilder(context, XyMusicDatabase::class.java)
                .allowMainThreadQueries()
                .build()
        val httpClient = OkHttpClient()
        try {
            val cleaner =
                ServerCacheCleaner(
                    context = context,
                    sessionProvider =
                    object : AppSessionProvider {
                        override val sessionState: StateFlow<AppSessionState> =
                            MutableStateFlow<AppSessionState>(AppSessionState.SignedOut)

                        override suspend fun restoreSession() = Unit
                    },
                    sessionInvalidator = SessionInvalidator { },
                    database = database,
                    playerRepository = NoOpPlayerRepository(),
                    playbackGrantRepository =
                    object : PlaybackGrantRepository {
                        override suspend fun get(trackId: String, forceRefresh: Boolean): PlayerResult<PlaybackGrant> =
                            error("Not used")

                        override fun invalidate(trackId: String) = Unit

                        override fun clear() = Unit
                    },
                    offlineMediaCache = NoOpOfflineMediaCache(),
                    searchOverviewStore =
                    object : SearchOverviewStore {
                        override fun observe(query: SearchQuery): Flow<SearchOverview?> = emptyFlow()

                        override suspend fun replace(query: SearchQuery, overview: SearchOverviewRemote) = Unit
                    },
                    profileMemoryCache = ProfileMemoryCache(ServerRuntimeCoordinator()),
                    apiHttpClient = httpClient,
                    authHttpClient = httpClient,
                    mediaHttpClient = httpClient,
                )

            cleaner.clearAllServerData()

            assertThat(cachedArtworkAmbientPaletteForTest(cacheKey)).isNull()
        } finally {
            database.close()
        }
    }

    private class NoOpOfflineMediaCache : OfflineMediaCache {
        override fun pin(cacheKey: String) = Unit

        override fun unpin(cacheKey: String) = Unit

        override fun promotePin(cacheKey: String) = Unit

        override fun isFullyCached(cacheKey: String, contentLength: Long): Boolean = false

        override fun cachedContentLength(cacheKey: String): Long? = null

        override val cache: Cache
            get() = error("Not used")

        override suspend fun remove(cacheKey: String) = Unit

        override suspend fun clear() = Unit
    }

    private class NoOpPlayerRepository : PlayerRepository {
        override val state: StateFlow<PlayerState> = MutableStateFlow(PlayerState())
        override val events: Flow<PlayerEvent> = emptyFlow()

        override suspend fun connect(): PlayerResult<Unit> = PlayerResult.Success(Unit)

        override suspend fun disconnect() = Unit

        override suspend fun setQueue(
            items: List<PlayerQueueItem>,
            startQueueItemId: String?,
            startPositionMs: Long,
            playWhenReady: Boolean,
        ): PlayerResult<Unit> = PlayerResult.Success(Unit)

        override suspend fun addToQueue(items: List<PlayerQueueItem>): PlayerResult<Unit> = PlayerResult.Success(Unit)

        override suspend fun removeFromQueue(queueItemId: String): PlayerResult<Unit> = PlayerResult.Success(Unit)

        override suspend fun moveQueueItem(queueItemId: String, newIndex: Int): PlayerResult<Unit> =
            PlayerResult.Success(Unit)

        override suspend fun clearQueue(): PlayerResult<Unit> = PlayerResult.Success(Unit)

        override suspend fun play(): PlayerResult<Unit> = PlayerResult.Success(Unit)

        override suspend fun pause(): PlayerResult<Unit> = PlayerResult.Success(Unit)

        override suspend fun seekTo(positionMs: Long): PlayerResult<Unit> = PlayerResult.Success(Unit)

        override suspend fun seekToQueueItem(queueItemId: String, positionMs: Long): PlayerResult<Unit> =
            PlayerResult.Success(Unit)

        override suspend fun skipToNext(): PlayerResult<Unit> = PlayerResult.Success(Unit)

        override suspend fun skipToPrevious(): PlayerResult<Unit> = PlayerResult.Success(Unit)

        override suspend fun setRepeatMode(mode: RepeatMode): PlayerResult<Unit> = PlayerResult.Success(Unit)

        override suspend fun setShuffleEnabled(enabled: Boolean): PlayerResult<Unit> = PlayerResult.Success(Unit)

        override suspend fun setPlaybackSpeed(speed: Float): PlayerResult<Unit> = PlayerResult.Success(Unit)

        override suspend fun setSleepTimer(durationMs: Long?): PlayerResult<Unit> = PlayerResult.Success(Unit)
    }
}
