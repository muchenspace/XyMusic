@file:Suppress("DEPRECATION")

package com.xymusic.app.feature.player.service

import android.content.Context
import android.graphics.Bitmap
import android.graphics.Canvas
import android.net.Uri
import android.os.SystemClock
import androidx.core.content.ContextCompat
import androidx.core.graphics.createBitmap
import androidx.media3.common.MediaMetadata
import androidx.media3.common.util.BitmapLoader
import androidx.media3.common.util.UnstableApi
import androidx.media3.session.SimpleBitmapLoader
import coil3.SingletonImageLoader
import coil3.request.ImageRequest
import coil3.request.ImageResult
import coil3.request.SuccessResult
import coil3.request.allowHardware
import coil3.toBitmap
import com.google.common.util.concurrent.Futures
import com.google.common.util.concurrent.ListenableFuture
import com.google.common.util.concurrent.SettableFuture
import com.xymusic.app.R
import com.xymusic.app.feature.player.data.media.PlaybackMediaMetadata
import java.util.concurrent.Executor
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch

@UnstableApi
internal class PlaybackArtworkBitmapLoader(
    private val context: Context,
    private val scope: CoroutineScope,
    private val executeImageRequest: suspend (ImageRequest) -> ImageResult = { request ->
        SingletonImageLoader.get(context).execute(request)
    },
    fallbackBitmapProvider: () -> Bitmap = { createPlaybackArtworkFallbackBitmap(context) },
    private val bitmapDecoder: BitmapLoader = SimpleBitmapLoader(),
    private val elapsedRealtimeMs: () -> Long = SystemClock::elapsedRealtime,
    private val failedRequestRetryDelayMs: Long = FAILED_REQUEST_RETRY_DELAY_MS,
) : BitmapLoader {
    private val fallbackBitmap by lazy(LazyThreadSafetyMode.SYNCHRONIZED, fallbackBitmapProvider)
    private val requestLock = Any()
    private val inFlightRequests = mutableMapOf<String, ListenableFuture<Bitmap>>()

    private var lastFailedRequest: FailedArtworkRequest? = null

    override fun supportsMimeType(mimeType: String): Boolean = bitmapDecoder.supportsMimeType(mimeType)

    override fun decodeBitmap(data: ByteArray): ListenableFuture<Bitmap> = bitmapDecoder.decodeBitmap(data)

    override fun loadBitmap(uri: Uri): ListenableFuture<Bitmap> = loadArtwork(uri, cacheKey = null)

    override fun loadBitmapFromMetadata(metadata: MediaMetadata): ListenableFuture<Bitmap> {
        metadata.artworkData?.let { artworkData ->
            return Futures.catching(
                decodeBitmap(artworkData),
                Exception::class.java,
                { fallbackBitmap },
                DIRECT_EXECUTOR,
            )
        }
        val artworkUri = metadata.artworkUri ?: return Futures.immediateFuture(fallbackBitmap)
        val cacheKey =
            metadata.extras?.getString(PlaybackMediaMetadata.EXTRA_ARTWORK_CACHE_KEY)
        return loadArtwork(artworkUri, cacheKey)
    }

    private fun loadArtwork(uri: Uri, cacheKey: String?): ListenableFuture<Bitmap> {
        val stableCacheKey = cacheKey?.takeIf(String::isNotBlank)
        val requestIdentity = "${stableCacheKey.orEmpty()}\u0000$uri"
        return synchronized(requestLock) {
            val failedRequest = lastFailedRequest
            if (
                failedRequest?.identity == requestIdentity &&
                elapsedRealtimeMs() < failedRequest.retryAtElapsedRealtimeMs
            ) {
                return@synchronized Futures.immediateFuture(fallbackBitmap)
            }
            inFlightRequests[requestIdentity]?.let { return@synchronized it }
            startArtworkLoad(uri, stableCacheKey, requestIdentity)
        }
    }

    private fun startArtworkLoad(
        uri: Uri,
        stableCacheKey: String?,
        requestIdentity: String,
    ): ListenableFuture<Bitmap> {
        val future = SettableFuture.create<Bitmap>()
        inFlightRequests[requestIdentity] = future
        val job =
            scope.launch(Dispatchers.IO) {
                try {
                    val request =
                        ImageRequest
                            .Builder(context)
                            .data(uri.toString())
                            .size(ARTWORK_REQUEST_SIZE_PX)
                            .allowHardware(false)
                            .applyStableArtworkCacheKey(stableCacheKey)
                            .build()
                    val result = executeImageRequest(request)
                    val bitmap = (result as? SuccessResult)?.image?.toBitmap()
                    if (bitmap == null) {
                        recordFailedRequest(requestIdentity)
                        future.set(fallbackBitmap)
                    } else {
                        clearFailedRequest(requestIdentity)
                        future.set(bitmap)
                    }
                } catch (failure: CancellationException) {
                    future.cancel(false)
                    throw failure
                } catch (_: Exception) {
                    recordFailedRequest(requestIdentity)
                    future.set(fallbackBitmap)
                }
            }
        job.invokeOnCompletion { failure ->
            if (failure != null && !future.isDone) {
                if (failure is CancellationException) {
                    future.cancel(false)
                } else {
                    future.setException(failure)
                }
            }
        }
        future.addListener(
            {
                synchronized(requestLock) {
                    if (inFlightRequests[requestIdentity] === future) {
                        inFlightRequests.remove(requestIdentity)
                    }
                }
                if (future.isCancelled) job.cancel()
            },
            DIRECT_EXECUTOR,
        )
        return future
    }

    private fun recordFailedRequest(requestIdentity: String) {
        synchronized(requestLock) {
            lastFailedRequest =
                FailedArtworkRequest(
                    identity = requestIdentity,
                    retryAtElapsedRealtimeMs = elapsedRealtimeMs() + failedRequestRetryDelayMs,
                )
        }
    }

    private fun clearFailedRequest(requestIdentity: String) {
        synchronized(requestLock) {
            if (lastFailedRequest?.identity == requestIdentity) {
                lastFailedRequest = null
            }
        }
    }

    private companion object {
        const val ARTWORK_REQUEST_SIZE_PX = 512
        const val FAILED_REQUEST_RETRY_DELAY_MS = 30_000L
        val DIRECT_EXECUTOR: Executor = Executor(Runnable::run)
    }
}

private fun ImageRequest.Builder.applyStableArtworkCacheKey(cacheKey: String?): ImageRequest.Builder = apply {
    cacheKey?.let { stableKey ->
        memoryCacheKey(stableKey)
        diskCacheKey(stableKey)
    }
}

private data class FailedArtworkRequest(val identity: String, val retryAtElapsedRealtimeMs: Long)

private fun createPlaybackArtworkFallbackBitmap(context: Context): Bitmap {
    val bitmap = createBitmap(FALLBACK_ARTWORK_SIZE_PX, FALLBACK_ARTWORK_SIZE_PX)
    val canvas = Canvas(bitmap)
    ContextCompat.getDrawable(context, R.drawable.xymusic)?.let { icon ->
        icon.setBounds(0, 0, FALLBACK_ARTWORK_SIZE_PX, FALLBACK_ARTWORK_SIZE_PX)
        icon.draw(canvas)
    }
    return bitmap
}

private const val FALLBACK_ARTWORK_SIZE_PX = 256
