package com.xymusic.app.core.ui.component

import android.content.Context
import android.content.res.Resources
import android.util.LruCache
import androidx.compose.runtime.Composable
import androidx.compose.runtime.produceState
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalResources
import androidx.core.graphics.drawable.toBitmap
import androidx.palette.graphics.Palette
import coil3.SingletonImageLoader
import coil3.asDrawable
import coil3.request.ImageRequest
import coil3.request.SuccessResult
import com.xymusic.app.core.common.runCatchingPreservingCancellation
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

@Composable
fun rememberArtworkAmbientColor(artworkUrl: String?, cacheKey: String?): Color? {
    val context = LocalContext.current
    val resources = LocalResources.current
    val state =
        produceState<Color?>(
            initialValue = null,
            key1 = artworkUrl,
            key2 = cacheKey,
            key3 = resources,
        ) {
            value =
                if (artworkUrl.isNullOrBlank()) {
                    null
                } else {
                    val artworkIdentity = stableArtworkCacheKey(cacheKey) ?: artworkUrl
                    cachedArtworkAmbientColor(artworkIdentity)
                        ?: run {
                            // Let the first player frame use the fallback background while artwork loads.
                            withFrameNanos { }
                            extractArtworkAmbientColor(
                                artworkUrl = artworkUrl,
                                cacheKey = cacheKey,
                                artworkIdentity = artworkIdentity,
                                context = context,
                                resources = resources,
                            )
                        }
                }
        }
    return state.value
}

private suspend fun extractArtworkAmbientColor(
    artworkUrl: String,
    cacheKey: String?,
    artworkIdentity: String,
    context: Context,
    resources: Resources,
): Color? = runCatchingPreservingCancellation {
    val bitmap =
        withContext(Dispatchers.IO) {
            val loader = SingletonImageLoader.get(context)
            val request =
                ImageRequest
                    .Builder(context)
                    .data(artworkUrl)
                    // Keep the small palette request separate from the full-size artwork entry.
                    .applyAmbientArtworkCacheKey(cacheKey)
                    .size(128)
                    .build()
            val result = loader.execute(request)
            if (result is SuccessResult) {
                result.image.asDrawable(resources).toBitmap(128, 128)
            } else {
                null
            }
        }
    bitmap?.let { artworkBitmap ->
        withContext(Dispatchers.Default) {
            val palette = Palette.from(artworkBitmap).generate()
            val swatch =
                palette.darkVibrantSwatch
                    ?: palette.darkMutedSwatch
                    ?: palette.vibrantSwatch
                    ?: palette.dominantSwatch
            swatch?.rgb
                ?.also { color -> cacheArtworkAmbientColor(artworkIdentity, color) }
                ?.let(::Color)
        }
    }
}.getOrNull()

private val artworkAmbientColorCache = LruCache<String, Int>(64)

private fun cachedArtworkAmbientColor(key: String): Color? = synchronized(artworkAmbientColorCache) {
    artworkAmbientColorCache[key]?.let(::Color)
}

private fun cacheArtworkAmbientColor(key: String, color: Int) {
    synchronized(artworkAmbientColorCache) {
        artworkAmbientColorCache.put(key, color)
    }
}

private fun ImageRequest.Builder.applyAmbientArtworkCacheKey(cacheKey: String?): ImageRequest.Builder = apply {
    stableArtworkCacheKey(cacheKey)?.let { stableKey ->
        memoryCacheKey("$stableKey:ambient-128")
        // Reuse the encoded disk entry; only the decoded memory entry needs a distinct size key.
        diskCacheKey(stableKey)
    }
}
