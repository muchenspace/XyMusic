package com.xymusic.app.core.ui.component

import androidx.annotation.DrawableRes
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.MusicNote
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import coil3.compose.AsyncImage
import coil3.request.ImageRequest
import com.xymusic.app.ui.theme.xyColors

@Composable
fun MediaArtwork(
    url: String?,
    cacheKey: String?,
    contentDescription: String?,
    modifier: Modifier = Modifier,
    @DrawableRes fallbackImageRes: Int? = null,
    fallbackIcon: ImageVector = Icons.Outlined.MusicNote,
    shape: Shape = RoundedCornerShape(8.dp),
    contentScale: ContentScale = ContentScale.Crop,
    fallbackModifier: Modifier = Modifier,
    fallbackIconFraction: Float = 0.32f,
    fallbackTint: Color? = null,
    imageModifier: Modifier = Modifier,
    elevation: Dp = 0.dp,
) {
    require(fallbackIconFraction in 0f..1f) { "fallbackIconFraction must be between 0 and 1" }
    val request = rememberArtworkImageRequest(url, cacheKey)
    val surface = MaterialTheme.colorScheme.surfaceContainer
    val surfaceHigh = MaterialTheme.colorScheme.surfaceContainerHigh
    val gradientBg =
        remember(surface, surfaceHigh) {
            Brush.verticalGradient(colors = listOf(surface, surfaceHigh))
        }
    Box(
        modifier =
        modifier
            .then(if (elevation > 0.dp) Modifier.shadow(elevation, shape) else Modifier)
            .clip(shape)
            .background(gradientBg),
        contentAlignment = Alignment.Center,
    ) {
        if (fallbackImageRes != null) {
            Image(
                painter = painterResource(fallbackImageRes),
                contentDescription = null,
                modifier =
                Modifier
                    .fillMaxSize()
                    .then(fallbackModifier),
                contentScale = ContentScale.Crop,
            )
        } else {
            Icon(
                imageVector = fallbackIcon,
                contentDescription = null,
                modifier =
                Modifier
                    .fillMaxSize(fallbackIconFraction)
                    .then(fallbackModifier),
                tint = fallbackTint ?: MaterialTheme.xyColors.onSurfaceDim,
            )
        }
        if (request != null) {
            AsyncImage(
                model = request,
                contentDescription = contentDescription,
                modifier =
                Modifier
                    .fillMaxSize()
                    .then(imageModifier),
                contentScale = contentScale,
            )
        }
    }
}

@Composable
internal fun rememberArtworkImageRequest(url: String?, cacheKey: String?): ImageRequest? {
    val context = LocalContext.current
    return remember(context, url, cacheKey) {
        url?.takeIf(String::isNotBlank)?.let { source ->
            ImageRequest
                .Builder(context)
                .data(source)
                .applyStableArtworkCacheKey(cacheKey)
                .build()
        }
    }
}

internal fun ImageRequest.Builder.applyStableArtworkCacheKey(cacheKey: String?): ImageRequest.Builder = apply {
    stableArtworkCacheKey(cacheKey)?.let { stableKey ->
        memoryCacheKey(stableKey)
        diskCacheKey(stableKey)
    }
}

internal fun stableArtworkCacheKey(cacheKey: String?): String? = cacheKey?.takeIf(String::isNotBlank)
