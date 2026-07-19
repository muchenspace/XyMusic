package com.xymusic.app.core.ui.component

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Image
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.dp
import coil3.compose.AsyncImage
import com.xymusic.app.R
import kotlinx.coroutines.isActive

@Composable
fun VinylRecord(artworkUrl: String?, cacheKey: String?, isPlaying: Boolean, modifier: Modifier = Modifier) {
    val rotation = remember { Animatable(0f) }
    val artworkRequest = rememberArtworkImageRequest(artworkUrl, cacheKey)

    LaunchedEffect(isPlaying) {
        if (!isPlaying) {
            rotation.stop()
            return@LaunchedEffect
        }
        while (isActive) {
            val startRotation = rotation.value % FULL_ROTATION_DEGREES
            rotation.snapTo(startRotation)
            rotation.animateTo(
                targetValue = startRotation + FULL_ROTATION_DEGREES,
                animationSpec = tween(durationMillis = ROTATION_DURATION_MILLIS, easing = LinearEasing),
            )
        }
    }

    Box(
        modifier =
        modifier
            .aspectRatio(1f)
            .graphicsLayer { rotationZ = rotation.value },
        contentAlignment = Alignment.Center,
    ) {
        VinylDisc()
        Box(
            modifier =
            Modifier
                .fillMaxSize(0.62f)
                .clip(CircleShape),
            contentAlignment = Alignment.Center,
        ) {
            Image(
                painter = painterResource(R.drawable.xymusic),
                contentDescription = null,
                modifier = Modifier.fillMaxSize(),
                contentScale = ContentScale.Crop,
            )
            if (artworkRequest != null) {
                AsyncImage(
                    model = artworkRequest,
                    contentDescription = null,
                    modifier = Modifier.fillMaxSize(),
                    contentScale = ContentScale.Crop,
                )
            }
        }
        Box(
            modifier =
            Modifier
                .size(16.dp)
                .clip(CircleShape)
                .background(Color(0xFF1A1A1A)),
        )
    }
}

private const val FULL_ROTATION_DEGREES = 360f
private const val ROTATION_DURATION_MILLIS = 8_000

@Composable
private fun VinylDisc(modifier: Modifier = Modifier) {
    Canvas(modifier = modifier.fillMaxSize()) {
        val center = Offset(size.width / 2f, size.height / 2f)
        val radius = size.minDimension / 2f

        drawCircle(
            brush =
            Brush.radialGradient(
                colors =
                listOf(
                    Color(0xFF2A2A2A),
                    Color(0xFF1A1A1A),
                    Color(0xFF0A0A0A),
                    Color(0xFF1A1A1A),
                    Color(0xFF2A2A2A),
                ),
                center = center,
                radius = radius,
            ),
            radius = radius,
            center = center,
        )

        for (i in 1..15) {
            val ringRadius = radius * (0.7f + i * 0.018f)
            drawCircle(
                color = Color(0x08000000),
                radius = ringRadius,
                center = center,
                style = Stroke(width = 1.5.dp.toPx()),
            )
        }

        drawCircle(
            color = Color(0x1FFFFFFF),
            radius = radius * 0.92f,
            center = center,
            style = Stroke(width = 0.5.dp.toPx()),
        )
    }
}
