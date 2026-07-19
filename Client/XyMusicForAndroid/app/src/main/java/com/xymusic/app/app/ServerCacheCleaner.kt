package com.xymusic.app.app

import android.content.Context
import androidx.media3.common.util.UnstableApi
import coil3.SingletonImageLoader
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.network.ApiHttpClient
import com.xymusic.app.core.network.AuthHttpClient
import com.xymusic.app.core.network.MediaHttpClient
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionInvalidator
import com.xymusic.app.feature.player.data.media.PlaybackCache
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import com.xymusic.app.feature.player.domain.PlayerRepository
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.search.data.SearchOverviewStore
import com.xymusic.app.feature.settings.data.ProfileMemoryCache
import dagger.hilt.android.qualifiers.ApplicationContext
import javax.inject.Inject
import javax.inject.Singleton
import okhttp3.OkHttpClient

/** Clears every cache whose contents may have originated from the previous server. */
@Singleton
@UnstableApi
class ServerCacheCleaner
@Inject
constructor(
    @ApplicationContext private val context: Context,
    private val sessionProvider: AppSessionProvider,
    private val sessionInvalidator: SessionInvalidator,
    private val database: XyMusicDatabase,
    private val playerRepository: PlayerRepository,
    private val playbackGrantRepository: PlaybackGrantRepository,
    private val playbackCache: PlaybackCache,
    private val searchOverviewStore: SearchOverviewStore,
    private val profileMemoryCache: ProfileMemoryCache,
    @ApiHttpClient private val apiHttpClient: OkHttpClient,
    @AuthHttpClient private val authHttpClient: OkHttpClient,
    @MediaHttpClient private val mediaHttpClient: OkHttpClient,
) : ServerDataCleaner {
    override suspend fun clearAllServerData() {
        val failures = mutableListOf<Throwable>()

        suspend fun attempt(block: suspend () -> Unit) {
            try {
                block()
            } catch (failure: Exception) {
                failures += failure
            }
        }

        attempt {
            when (val result = playerRepository.pause()) {
                is PlayerResult.Success -> Unit
                is PlayerResult.Failure -> error("Unable to pause playback: ${result.failure}")
            }
        }
        attempt {
            when (val result = playerRepository.clearQueue()) {
                is PlayerResult.Success -> Unit
                is PlayerResult.Failure -> error("Unable to clear playback queue: ${result.failure}")
            }
        }
        attempt { playerRepository.disconnect() }

        attempt { cancelAndEvict(apiHttpClient) }
        attempt { cancelAndEvict(authHttpClient) }
        attempt { cancelAndEvict(mediaHttpClient) }

        val ownerUserId = (sessionProvider.sessionState.value as? AppSessionState.SignedIn)?.userId
        attempt { sessionInvalidator.invalidateSession(ownerUserId) }
        attempt { database.clearAllTables() }
        attempt { playbackCache.clear() }
        attempt { playbackGrantRepository.clear() }
        attempt { searchOverviewStore.clearMemory() }
        attempt { profileMemoryCache.clear() }
        attempt {
            val imageLoader = SingletonImageLoader.get(context)
            imageLoader.memoryCache?.clear()
            imageLoader.diskCache?.clear()
        }

        if (failures.isNotEmpty()) throw ServerCacheCleanupException(failures)
    }

    private fun cancelAndEvict(client: OkHttpClient) {
        client.dispatcher.cancelAll()
        client.connectionPool.evictAll()
    }
}

fun interface ServerDataCleaner {
    suspend fun clearAllServerData()
}

private class ServerCacheCleanupException(failures: List<Throwable>) :
    IllegalStateException("Unable to completely clear data from the previous server", failures.first()) {
    init {
        failures.drop(1).forEach(::addSuppressed)
    }
}
