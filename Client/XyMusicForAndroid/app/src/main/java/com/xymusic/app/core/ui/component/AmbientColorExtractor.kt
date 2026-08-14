package com.xymusic.app.core.ui.component

import android.content.Context
import android.content.res.Resources
import android.util.LruCache
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
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

@Immutable
data class ArtworkAmbientPalette(val primary: Color, val secondary: Color, val highlight: Color)

@Composable
fun rememberArtworkAmbientColor(artworkUrl: String?, cacheKey: String?): Color? =
    rememberArtworkAmbientPalette(artworkUrl, cacheKey)?.primary

@Composable
fun rememberArtworkAmbientPalette(artworkUrl: String?, cacheKey: String?): ArtworkAmbientPalette? {
    val context = LocalContext.current
    val resources = LocalResources.current
    val artworkIdentity = remember(artworkUrl, cacheKey) {
        if (artworkUrl.isNullOrBlank()) null else (stableArtworkCacheKey(cacheKey) ?: artworkUrl)
    }
    val initialCached = remember(artworkIdentity) {
        artworkIdentity?.let { cachedArtworkAmbientPalette(it) }
    }
    val state =
        produceState<ArtworkAmbientPalette?>(
            initialValue = initialCached,
            key1 = artworkUrl,
            key2 = cacheKey,
            key3 = resources,
        ) {
            if (artworkUrl.isNullOrBlank()) {
                value = null
            } else if (value == null) {
                val identity = stableArtworkCacheKey(cacheKey) ?: artworkUrl
                value = cachedArtworkAmbientPalette(identity)
                    ?: extractArtworkAmbientColor(
                        artworkUrl = artworkUrl,
                        cacheKey = cacheKey,
                        artworkIdentity = identity,
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
): ArtworkAmbientPalette? = runCatchingPreservingCancellation {
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
            val primarySwatch =
                palette.dominantSwatch
                    ?: palette.vibrantSwatch
                    ?: palette.darkVibrantSwatch
                    ?: palette.darkMutedSwatch
                    ?: palette.mutedSwatch
            val secondarySwatch =
                palette.vibrantSwatch
                    ?: palette.darkVibrantSwatch
                    ?: palette.lightVibrantSwatch
                    ?: palette.mutedSwatch
                    ?: primarySwatch
            val highlightSwatch =
                palette.lightVibrantSwatch
                    ?: palette.lightMutedSwatch
                    ?: palette.vibrantSwatch
                    ?: secondarySwatch
            primarySwatch?.let {
                ArtworkAmbientPalette(
                    primary = Color(it.rgb),
                    secondary = Color(secondarySwatch?.rgb ?: it.rgb),
                    highlight = Color(highlightSwatch?.rgb ?: it.rgb),
                ).also { colors -> cacheArtworkAmbientPalette(artworkIdentity, colors) }
            }
        }
    }
}.getOrNull()

private val artworkAmbientPaletteCache = LruCache<String, ArtworkAmbientPalette>(64)

private fun cachedArtworkAmbientPalette(key: String): ArtworkAmbientPalette? =
    synchronized(artworkAmbientPaletteCache) {
        artworkAmbientPaletteCache[key]
    }

private fun cacheArtworkAmbientPalette(key: String, colors: ArtworkAmbientPalette) {
    synchronized(artworkAmbientPaletteCache) {
        artworkAmbientPaletteCache.put(key, colors)
    }
}

private fun ImageRequest.Builder.applyAmbientArtworkCacheKey(cacheKey: String?): ImageRequest.Builder = apply {
    stableArtworkCacheKey(cacheKey)?.let { stableKey ->
        memoryCacheKey("$stableKey:ambient-128")
        // Reuse the encoded disk entry; only the decoded memory entry needs a distinct size key.
        diskCacheKey(stableKey)
    }
}
