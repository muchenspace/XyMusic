package com.xymusic.app.core.ui.component

import android.content.Context
import android.content.res.Resources
import android.util.LruCache
import androidx.compose.runtime.Composable
import androidx.compose.runtime.produceState
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
                    artworkAmbientColorCache[artworkIdentity]?.let(::Color)
                        ?: extractArtworkAmbientColor(
                            artworkUrl = artworkUrl,
                            cacheKey = cacheKey,
                            artworkIdentity = artworkIdentity,
                            context = context,
                            resources = resources,
                        )
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
                    .applyStableArtworkCacheKey(cacheKey)
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
                ?.also { artworkAmbientColorCache.put(artworkIdentity, it) }
                ?.let(::Color)
        }
    }
}.getOrNull()

private val artworkAmbientColorCache = LruCache<String, Int>(64)
