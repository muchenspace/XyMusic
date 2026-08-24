package com.xymusic.app.feature.player.presentation

import com.xymusic.app.ui.theme.XyMotion
import kotlin.math.roundToInt

internal enum class PlayerDismissTarget {
    Restore,
    Dismiss,
}

internal fun updatePlayerDismissOffset(currentOffsetPx: Float, dragDeltaPx: Float, maxOffsetPx: Float): Float {
    require(maxOffsetPx > 0f)

    return (currentOffsetPx + dragDeltaPx).coerceIn(0f, maxOffsetPx)
}

internal fun resolvePlayerDismissTarget(
    offsetPx: Float,
    releaseVelocityPxPerSecond: Float,
    distanceThresholdPx: Float,
    velocityThresholdPxPerSecond: Float,
): PlayerDismissTarget {
    require(distanceThresholdPx > 0f)
    require(velocityThresholdPxPerSecond > 0f)

    return when {
        releaseVelocityPxPerSecond <= -velocityThresholdPxPerSecond -> PlayerDismissTarget.Restore
        releaseVelocityPxPerSecond >= velocityThresholdPxPerSecond -> PlayerDismissTarget.Dismiss
        offsetPx >= distanceThresholdPx -> PlayerDismissTarget.Dismiss
        else -> PlayerDismissTarget.Restore
    }
}

internal fun playerDismissDurationMillis(
    offsetPx: Float,
    maxOffsetPx: Float,
    releaseVelocityPxPerSecond: Float = 0f,
): Int {
    require(maxOffsetPx > 0f)

    val remainingDistance = (maxOffsetPx - offsetPx).coerceIn(0f, maxOffsetPx)
    if (remainingDistance == 0f) return XyMotion.Instant

    val remainingFraction = remainingDistance / maxOffsetPx
    val distanceDuration =
        XyMotion.Fast +
            ((XyMotion.Standard - XyMotion.Fast) * remainingFraction).roundToInt()
    val velocityDuration =
        if (releaseVelocityPxPerSecond > 0f) {
            ((remainingDistance / releaseVelocityPxPerSecond) * MILLIS_PER_SECOND)
                .roundToInt()
                .coerceAtLeast(XyMotion.Fast)
        } else {
            XyMotion.Standard
        }
    return minOf(distanceDuration, velocityDuration).coerceIn(XyMotion.Fast, XyMotion.Standard)
}

private const val MILLIS_PER_SECOND = 1_000f
