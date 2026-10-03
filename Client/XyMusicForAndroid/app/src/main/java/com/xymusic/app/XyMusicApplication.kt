package com.xymusic.app

import android.app.Application
import android.os.StrictMode
import androidx.hilt.work.HiltWorkerFactory
import androidx.work.Configuration
import coil3.ImageLoader
import coil3.PlatformContext
import coil3.SingletonImageLoader
import coil3.network.okhttp.OkHttpNetworkFetcherFactory
import com.xymusic.app.core.network.MediaHttpClient
import dagger.Lazy
import dagger.hilt.android.HiltAndroidApp
import javax.inject.Inject
import okhttp3.OkHttpClient

@HiltAndroidApp
class XyMusicApplication :
    Application(),
    Configuration.Provider,
    SingletonImageLoader.Factory {
    @Inject
    lateinit var workerFactory: Lazy<HiltWorkerFactory>

    // Lazy so the network graph is not built during Application.onCreate; the
    // first image request resolves it on a background thread.
    @Inject
    @MediaHttpClient
    lateinit var mediaHttpClient: Lazy<OkHttpClient>

    override fun onCreate() {
        super.onCreate()
        if (BuildConfig.DEBUG) {
            StrictMode.setThreadPolicy(
                StrictMode.ThreadPolicy
                    .Builder()
                    .detectDiskReads()
                    .detectDiskWrites()
                    .penaltyLog()
                    .build(),
            )
        }
    }

    override fun newImageLoader(context: PlatformContext): ImageLoader = ImageLoader.Builder(context)
        .components {
            // Artwork is served from the same public asset endpoints as
            // playback media, so Coil reuses that client to inherit AppDns
            // (custom DoH/UDP resolution) and the shared connection pool.
            add(OkHttpNetworkFetcherFactory(callFactory = { mediaHttpClient.get() }))
        }
        .build()

    override val workManagerConfiguration: Configuration
        get() =
            Configuration
                .Builder()
                .setWorkerFactory(workerFactory.get())
                .build()
}
