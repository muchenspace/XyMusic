package com.xymusic.app.app.navigation

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.tween
import androidx.compose.foundation.gestures.Orientation
import androidx.compose.foundation.gestures.draggable
import androidx.compose.foundation.gestures.rememberDraggableState
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import com.xymusic.app.feature.player.presentation.PlayerDismissTarget
import com.xymusic.app.feature.player.presentation.playerDismissDurationMillis
import com.xymusic.app.feature.player.presentation.resolvePlayerDismissTarget
import com.xymusic.app.feature.player.presentation.updatePlayerDismissOffset
import com.xymusic.app.ui.theme.XyMotion
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.launch

internal object PlayerTransitionTestTags {
    const val Surface = "player_transition_surface"
}

private enum class PlayerOverlayPhase {
    Hidden,
    Entering,
    Visible,
    Exiting,
}

private data class PlayerOverlayMetrics(
    val dismissTargetOffset: Float,
    val dismissThreshold: Float,
    val dismissVelocityThreshold: Float,
)

/**
 * Keeps the player above the navigation shell while a single layer offset drives opening,
 * closing, and drag dismissal. Frame-rate values stay inside [translationY] so they never
 * invalidate the player composition.
 */
@Stable
private class PlayerOverlayMotion(initialVisible: Boolean) {
    private val offsetY = Animatable(0f)
    private var phase by mutableStateOf(
        if (initialVisible) PlayerOverlayPhase.Visible else PlayerOverlayPhase.Hidden,
    )
    private var dragOffsetY by mutableFloatStateOf(0f)
    private var isDragging by mutableStateOf(false)
    private var dismissRequested by mutableStateOf(false)

    val shouldCompose: Boolean
        get() = phase != PlayerOverlayPhase.Hidden

    val dragEnabled: Boolean
        get() = !dismissRequested && phase != PlayerOverlayPhase.Exiting

    val startDragImmediately: Boolean
        get() = offsetY.isRunning

    val immersiveLandscape: Boolean
        get() = phase == PlayerOverlayPhase.Visible

    suspend fun updateVisibility(visible: Boolean, metrics: PlayerOverlayMetrics) {
        when {
            visible && phase == PlayerOverlayPhase.Hidden -> enterFromHidden(metrics)
            visible && phase == PlayerOverlayPhase.Exiting -> returnToVisible()
            !visible && phase != PlayerOverlayPhase.Hidden && phase != PlayerOverlayPhase.Exiting -> {
                exitToHidden(metrics)
            }
        }
    }

    fun requestDismiss(
        scope: CoroutineScope,
        metrics: PlayerOverlayMetrics,
        releaseVelocity: Float,
        onDismissRequest: () -> Unit,
    ) {
        if (dismissRequested || phase == PlayerOverlayPhase.Exiting) return

        val startingOffset = currentOffset()
        val dismissDuration =
            playerDismissDurationMillis(
                offsetPx = startingOffset,
                maxOffsetPx = metrics.dismissTargetOffset,
                releaseVelocityPxPerSecond = releaseVelocity,
            )
        dismissRequested = true
        phase = PlayerOverlayPhase.Exiting
        scope.launch(start = CoroutineStart.UNDISPATCHED) {
            // Cancels an in-flight entry before the parent updates visibility.
            offsetY.snapTo(startingOffset)
            isDragging = false
            onDismissRequest()
            animateExit(metrics.dismissTargetOffset, dismissDuration)
        }
    }

    fun dragBy(dragDelta: Float, metrics: PlayerOverlayMetrics) {
        if (!dragEnabled || !isDragging) return
        dragOffsetY =
            updatePlayerDismissOffset(
                currentOffsetPx = dragOffsetY,
                dragDeltaPx = dragDelta,
                maxOffsetPx = metrics.dismissTargetOffset,
            )
    }

    fun startDrag(scope: CoroutineScope, metrics: PlayerOverlayMetrics) {
        dragOffsetY = offsetY.value.coerceIn(0f, metrics.dismissTargetOffset)
        isDragging = true
        scope.launch { offsetY.stop() }
    }

    fun stopDrag(
        scope: CoroutineScope,
        metrics: PlayerOverlayMetrics,
        releaseVelocity: Float,
        onDismissRequest: () -> Unit,
    ) {
        if (!dragEnabled) return
        when (
            resolvePlayerDismissTarget(
                offsetPx = dragOffsetY,
                releaseVelocityPxPerSecond = releaseVelocity,
                distanceThresholdPx = metrics.dismissThreshold,
                velocityThresholdPxPerSecond = metrics.dismissVelocityThreshold,
            )
        ) {
            PlayerDismissTarget.Dismiss ->
                requestDismiss(
                    scope = scope,
                    metrics = metrics,
                    releaseVelocity = releaseVelocity,
                    onDismissRequest = onDismissRequest,
                )

            PlayerDismissTarget.Restore -> restore(scope, releaseVelocity)
        }
    }

    fun translationY(dismissTargetOffset: Float): Float = when {
        isDragging -> dragOffsetY
        phase == PlayerOverlayPhase.Hidden -> dismissTargetOffset
        else -> offsetY.value
    }

    private suspend fun enterFromHidden(metrics: PlayerOverlayMetrics) {
        dismissRequested = false
        phase = PlayerOverlayPhase.Entering
        offsetY.snapTo(metrics.dismissTargetOffset)
        offsetY.animateTo(
            targetValue = 0f,
            animationSpec = tween(
                durationMillis = XyMotion.Emphasized,
                easing = XyMotion.EmphasizedDecel,
            ),
        )
        phase = PlayerOverlayPhase.Visible
    }

    private suspend fun returnToVisible() {
        dismissRequested = false
        phase = PlayerOverlayPhase.Entering
        offsetY.animateTo(
            targetValue = 0f,
            animationSpec = tween(
                durationMillis = XyMotion.Standard,
                easing = XyMotion.EmphasizedDecel,
            ),
        )
        phase = PlayerOverlayPhase.Visible
    }

    private suspend fun exitToHidden(metrics: PlayerOverlayMetrics) {
        val startingOffset = currentOffset()
        val dismissDuration =
            playerDismissDurationMillis(
                offsetPx = startingOffset,
                maxOffsetPx = metrics.dismissTargetOffset,
            )
        isDragging = false
        phase = PlayerOverlayPhase.Exiting
        offsetY.snapTo(startingOffset)
        animateExit(metrics.dismissTargetOffset, dismissDuration)
    }

    private suspend fun animateExit(dismissTargetOffset: Float, durationMillis: Int) {
        offsetY.animateTo(
            targetValue = dismissTargetOffset,
            animationSpec = tween(
                durationMillis = durationMillis,
                easing = XyMotion.EmphasizedEasing,
            ),
        )
        phase = PlayerOverlayPhase.Hidden
        dismissRequested = false
    }

    private fun restore(scope: CoroutineScope, releaseVelocity: Float) {
        val startingOffset = dragOffsetY
        scope.launch {
            offsetY.snapTo(startingOffset)
            isDragging = false
            offsetY.animateTo(
                targetValue = 0f,
                animationSpec = XyMotion.SheetDismissSpring,
                initialVelocity = releaseVelocity,
            )
        }
    }

    private fun currentOffset(): Float = if (isDragging) dragOffsetY else offsetY.value
}

@Composable
internal fun PlayerTransitionOverlay(
    visible: Boolean,
    onDismissRequest: () -> Unit,
    modifier: Modifier = Modifier,
    content: @Composable (
        onDismiss: () -> Unit,
        dismissGestureModifier: Modifier,
        immersiveLandscape: Boolean,
    ) -> Unit,
) {
    val density = LocalDensity.current
    val scope = rememberCoroutineScope()
    val motion = remember { PlayerOverlayMotion(initialVisible = visible) }

    BoxWithConstraints(modifier = modifier.fillMaxSize()) {
        val dismissTargetOffset = with(density) { maxHeight.toPx() }.coerceAtLeast(1f)
        val metrics =
            PlayerOverlayMetrics(
                dismissTargetOffset = dismissTargetOffset,
                dismissThreshold = minOf(with(density) { 180.dp.toPx() }, dismissTargetOffset),
                dismissVelocityThreshold = with(density) { 1_000.dp.toPx() },
            )

        LaunchedEffect(visible, dismissTargetOffset) {
            motion.updateVisibility(visible = visible, metrics = metrics)
        }

        if (visible || motion.shouldCompose) {
            val dragState = rememberDraggableState { dragDelta -> motion.dragBy(dragDelta, metrics) }
            val dragModifier =
                Modifier.draggable(
                    state = dragState,
                    orientation = Orientation.Vertical,
                    enabled = motion.dragEnabled,
                    startDragImmediately = motion.startDragImmediately,
                    onDragStarted = { motion.startDrag(scope, metrics) },
                    onDragStopped = { releaseVelocity ->
                        motion.stopDrag(
                            scope = scope,
                            metrics = metrics,
                            releaseVelocity = releaseVelocity,
                            onDismissRequest = onDismissRequest,
                        )
                    },
                )
            Box(
                modifier =
                Modifier
                    .fillMaxSize()
                    .graphicsLayer {
                        translationY = motion.translationY(metrics.dismissTargetOffset)
                    }.testTag(PlayerTransitionTestTags.Surface),
            ) {
                content(
                    {
                        motion.requestDismiss(
                            scope = scope,
                            metrics = metrics,
                            releaseVelocity = 0f,
                            onDismissRequest = onDismissRequest,
                        )
                    },
                    dragModifier,
                    motion.immersiveLandscape,
                )
            }
        }
    }
}
