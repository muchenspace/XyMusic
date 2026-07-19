package com.xymusic.app.feature.player.service

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Color
import android.net.Uri
import android.os.Bundle
import androidx.media3.common.MediaMetadata
import androidx.media3.common.util.BitmapLoader
import androidx.media3.common.util.UnstableApi
import androidx.test.core.app.ApplicationProvider
import coil3.ImageLoader
import coil3.disk.DiskCache
import coil3.request.ImageRequest
import coil3.request.allowHardware
import com.google.common.truth.Truth.assertThat
import com.google.common.util.concurrent.Futures
import com.google.common.util.concurrent.ListenableFuture
import com.xymusic.app.feature.player.data.media.PlaybackMediaMetadata
import java.io.ByteArrayOutputStream
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okio.Path.Companion.toPath
import org.junit.After
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [27])
@UnstableApi
class PlaybackArtworkBitmapLoaderTest {
    private val context: Application = ApplicationProvider.getApplicationContext()
    private lateinit var scope: CoroutineScope
    private lateinit var server: MockWebServer
    private lateinit var imageLoader: ImageLoader

    @Before
    fun setUp() {
        scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        server = MockWebServer()
        server.start()
        val diskCacheDirectory =
            context.cacheDir.resolve("playback-artwork-test-${System.nanoTime()}")
        imageLoader =
            ImageLoader
                .Builder(context)
                .diskCache {
                    DiskCache
                        .Builder()
                        .directory(diskCacheDirectory.absolutePath.toPath())
                        .maxSizeBytes(TEST_DISK_CACHE_SIZE_BYTES)
                        .build()
                }.build()
    }

    @After
    fun tearDown() {
        scope.cancel()
        imageLoader.shutdown()
        server.shutdown()
    }

    @Test
    fun diskCacheServesExpiredUrlWithoutNetworkRequest() {
        val artworkBytes = pngBytes(Color.RED)
        val cacheKey = "artwork:asset-1:generation-2"
        val fallback = Bitmap.createBitmap(1, 1, Bitmap.Config.ARGB_8888)
        server.enqueue(
            MockResponse()
                .setHeader("Content-Type", "image/png")
                .setBody(okio.Buffer().write(artworkBytes)),
        )
        val loader = newLoader(fallback)

        val first =
            loader
                .loadBitmapFromMetadata(metadata(server.url("/fresh").toString(), cacheKey))
                .get(FUTURE_TIMEOUT_SECONDS, TimeUnit.SECONDS)
        imageLoader.memoryCache?.clear()
        server.enqueue(MockResponse().setResponseCode(403))

        val restored =
            loader
                .loadBitmapFromMetadata(metadata(server.url("/expired").toString(), cacheKey))
                .get(FUTURE_TIMEOUT_SECONDS, TimeUnit.SECONDS)

        assertThat(first).isNotSameInstanceAs(fallback)
        assertThat(restored).isNotSameInstanceAs(fallback)
        assertThat(server.requestCount).isEqualTo(1)
    }

    @Test
    fun cacheMiss403FallsBackAndDoesNotRepeatFailedRequest() {
        val fallback = Bitmap.createBitmap(2, 2, Bitmap.Config.ARGB_8888)
        fallback.eraseColor(Color.MAGENTA)
        server.enqueue(MockResponse().setResponseCode(403))
        val metadata = metadata(server.url("/expired").toString(), "artwork:missing")
        val loader = newLoader(fallback)

        val first =
            loader
                .loadBitmapFromMetadata(metadata)
                .get(FUTURE_TIMEOUT_SECONDS, TimeUnit.SECONDS)
        val repeated =
            loader
                .loadBitmapFromMetadata(metadata)
                .get(FUTURE_TIMEOUT_SECONDS, TimeUnit.SECONDS)

        assertThat(first).isSameInstanceAs(fallback)
        assertThat(repeated).isSameInstanceAs(fallback)
        assertThat(server.requestCount).isEqualTo(1)
    }

    @Test
    fun failedRequestRetriesAfterBackoffAndRecoversArtwork() {
        val fallback = Bitmap.createBitmap(2, 2, Bitmap.Config.ARGB_8888)
        val artworkBytes = pngBytes(Color.BLUE)
        var elapsedRealtimeMs = 1_000L
        server.enqueue(MockResponse().setResponseCode(403))
        server.enqueue(
            MockResponse()
                .setHeader("Content-Type", "image/png")
                .setBody(okio.Buffer().write(artworkBytes)),
        )
        val metadata = metadata(server.url("/eventually-available").toString(), "artwork:eventual")
        val loader =
            newLoader(
                fallback = fallback,
                elapsedRealtimeMs = { elapsedRealtimeMs },
                failedRequestRetryDelayMs = 500,
            )

        val first =
            loader
                .loadBitmapFromMetadata(metadata)
                .get(FUTURE_TIMEOUT_SECONDS, TimeUnit.SECONDS)
        val suppressedRetry =
            loader
                .loadBitmapFromMetadata(metadata)
                .get(FUTURE_TIMEOUT_SECONDS, TimeUnit.SECONDS)
        elapsedRealtimeMs += 500
        val recovered =
            loader
                .loadBitmapFromMetadata(metadata)
                .get(FUTURE_TIMEOUT_SECONDS, TimeUnit.SECONDS)

        assertThat(first).isSameInstanceAs(fallback)
        assertThat(suppressedRetry).isSameInstanceAs(fallback)
        assertThat(recovered).isNotSameInstanceAs(fallback)
        assertThat(server.requestCount).isEqualTo(2)
    }

    @Test
    fun artworkDataTakesPriorityOverArtworkUrl() {
        val embeddedArtwork = pngBytes(Color.GREEN)
        server.enqueue(MockResponse().setResponseCode(403))
        val metadata =
            MediaMetadata
                .Builder()
                .setArtworkData(embeddedArtwork, MediaMetadata.PICTURE_TYPE_FRONT_COVER)
                .setArtworkUri(Uri.parse(server.url("/must-not-load").toString()))
                .build()

        val bitmap =
            newLoader()
                .loadBitmapFromMetadata(metadata)
                .get(FUTURE_TIMEOUT_SECONDS, TimeUnit.SECONDS)

        assertThat(bitmap.getPixel(0, 0)).isEqualTo(Color.GREEN)
        assertThat(server.requestCount).isEqualTo(0)
    }

    @Test
    fun invalidArtworkDataFallsBackWithoutLoadingArtworkUrl() {
        val fallback = Bitmap.createBitmap(2, 2, Bitmap.Config.ARGB_8888)
        val failingDecoder =
            object : BitmapLoader {
                override fun supportsMimeType(mimeType: String): Boolean = true

                override fun decodeBitmap(data: ByteArray): ListenableFuture<Bitmap> =
                    Futures.immediateFailedFuture(IllegalArgumentException("Invalid artwork"))

                override fun loadBitmap(uri: Uri): ListenableFuture<Bitmap> = error("Not used")
            }
        server.enqueue(MockResponse().setResponseCode(200).setBody("must not load"))
        val metadata =
            MediaMetadata
                .Builder()
                .setArtworkData(byteArrayOf(1, 2, 3), MediaMetadata.PICTURE_TYPE_FRONT_COVER)
                .setArtworkUri(Uri.parse(server.url("/must-not-load").toString()))
                .build()

        val bitmap =
            PlaybackArtworkBitmapLoader(
                context = context,
                scope = scope,
                executeImageRequest = imageLoader::execute,
                fallbackBitmapProvider = { fallback },
                bitmapDecoder = failingDecoder,
            )
                .loadBitmapFromMetadata(metadata)
                .get(FUTURE_TIMEOUT_SECONDS, TimeUnit.SECONDS)

        assertThat(bitmap).isSameInstanceAs(fallback)
        assertThat(server.requestCount).isEqualTo(0)
    }

    @Test
    fun futureCancellationCancelsCoilRequestAndUsesStableSoftwareCacheKeys() = runBlocking {
        val requestStarted = CountDownLatch(1)
        val requestCancelled = CompletableDeferred<Unit>()
        val requestExecutions = AtomicInteger()
        lateinit var capturedRequest: ImageRequest
        val loader =
            PlaybackArtworkBitmapLoader(
                context = context,
                scope = scope,
                executeImageRequest = { request ->
                    requestExecutions.incrementAndGet()
                    capturedRequest = request
                    requestStarted.countDown()
                    try {
                        awaitCancellation()
                    } finally {
                        requestCancelled.complete(Unit)
                    }
                },
            )
        val cacheKey = "artwork:asset-2:generation-4"

        val future = loader.loadBitmapFromMetadata(metadata(server.url("/slow").toString(), cacheKey))
        val duplicateFuture =
            loader.loadBitmapFromMetadata(metadata(server.url("/slow").toString(), cacheKey))
        assertThat(requestStarted.await(FUTURE_TIMEOUT_SECONDS, TimeUnit.SECONDS)).isTrue()
        assertThat(duplicateFuture).isSameInstanceAs(future)
        assertThat(requestExecutions.get()).isEqualTo(1)
        future.cancel(true)

        withTimeout(TimeUnit.SECONDS.toMillis(FUTURE_TIMEOUT_SECONDS)) {
            requestCancelled.await()
        }
        assertThat(future.isCancelled).isTrue()
        assertThat(capturedRequest.memoryCacheKey).isEqualTo(cacheKey)
        assertThat(capturedRequest.diskCacheKey).isEqualTo(cacheKey)
        assertThat(capturedRequest.allowHardware).isFalse()
    }

    private fun newLoader(
        fallback: Bitmap? = null,
        elapsedRealtimeMs: () -> Long = android.os.SystemClock::elapsedRealtime,
        failedRequestRetryDelayMs: Long = 30_000,
    ): PlaybackArtworkBitmapLoader = PlaybackArtworkBitmapLoader(
        context = context,
        scope = scope,
        executeImageRequest = imageLoader::execute,
        fallbackBitmapProvider = { fallback ?: Bitmap.createBitmap(1, 1, Bitmap.Config.ARGB_8888) },
        elapsedRealtimeMs = elapsedRealtimeMs,
        failedRequestRetryDelayMs = failedRequestRetryDelayMs,
    )

    private fun metadata(artworkUrl: String, cacheKey: String): MediaMetadata = MediaMetadata
        .Builder()
        .setArtworkUri(Uri.parse(artworkUrl))
        .setExtras(
            Bundle().apply {
                putString(PlaybackMediaMetadata.EXTRA_ARTWORK_CACHE_KEY, cacheKey)
            },
        ).build()

    private fun pngBytes(color: Int): ByteArray {
        val bitmap = Bitmap.createBitmap(2, 2, Bitmap.Config.ARGB_8888)
        bitmap.eraseColor(color)
        return ByteArrayOutputStream().use { output ->
            check(bitmap.compress(Bitmap.CompressFormat.PNG, 100, output))
            output.toByteArray()
        }
    }

    private companion object {
        const val TEST_DISK_CACHE_SIZE_BYTES = 4L * 1024 * 1024
        const val FUTURE_TIMEOUT_SECONDS = 5L
    }
}
