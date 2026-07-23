package com.xymusic.app.feature.player.presentation

internal enum class PlayerDismissTarget {
    Restore,
    Dismiss,
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
