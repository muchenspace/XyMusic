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
import com.xymusic.app.feature.player.presentation.resolvePlayerDismissTarget
import com.xymusic.app.feature.player.presentation.updatePlayerDismissOffset
import com.xymusic.app.ui.theme.XyMotion
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

/**
 * Keeps the player above the navigation shell while a single offset drives opening, closing,
 * and drag dismissal. The underlying route never changes during this transition.
 */
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
    val offsetY = remember { Animatable(0f) }
    var phase by remember {
        mutableStateOf(if (visible) PlayerOverlayPhase.Visible else PlayerOverlayPhase.Hidden)
    }
    var dragOffsetY by remember { mutableFloatStateOf(0f) }
    var isDragging by remember { mutableStateOf(false) }
    var dismissRequested by remember { mutableStateOf(false) }

    BoxWithConstraints(modifier = modifier.fillMaxSize()) {
        val dismissTargetOffset = with(density) { maxHeight.toPx() }.coerceAtLeast(1f)
        val dismissThreshold = minOf(with(density) { 180.dp.toPx() }, dismissTargetOffset)
        val dismissVelocityThreshold = with(density) { 1_000.dp.toPx() }

        LaunchedEffect(visible, dismissTargetOffset) {
            when {
                visible && phase == PlayerOverlayPhase.Hidden -> {
                    dismissRequested = false
                    phase = PlayerOverlayPhase.Entering
                    offsetY.snapTo(dismissTargetOffset)
                    offsetY.animateTo(
                        targetValue = 0f,
                        animationSpec = tween(
                            durationMillis = XyMotion.Emphasized,
                            easing = XyMotion.EmphasizedDecel,
                        ),
                    )
                    phase = PlayerOverlayPhase.Visible
                }

                visible && phase == PlayerOverlayPhase.Exiting -> {
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

                !visible &&
                    phase != PlayerOverlayPhase.Hidden &&
                    phase != PlayerOverlayPhase.Exiting -> {
                    isDragging = false
                    phase = PlayerOverlayPhase.Exiting
                    offsetY.animateTo(
                        targetValue = dismissTargetOffset,
                        animationSpec = tween(
                            durationMillis = XyMotion.Standard,
                            easing = XyMotion.EmphasizedEasing,
                        ),
                    )
                    phase = PlayerOverlayPhase.Hidden
                    dismissRequested = false
                }
            }
        }

        if (visible || phase != PlayerOverlayPhase.Hidden) {
            val requestDismiss: () -> Unit = {
                if (!dismissRequested && phase != PlayerOverlayPhase.Exiting) {
                    val startingOffset =
                        if (isDragging) {
                            dragOffsetY
                        } else {
                            offsetY.value
                        }
                    dismissRequested = true
                    phase = PlayerOverlayPhase.Exiting
                    scope.launch(start = CoroutineStart.UNDISPATCHED) {
                        // Cancels an in-flight entry before the parent updates visibility.
                        offsetY.snapTo(startingOffset)
                        isDragging = false
                        onDismissRequest()
                        offsetY.animateTo(
                            targetValue = dismissTargetOffset,
                            animationSpec = tween(
                                durationMillis = XyMotion.Standard,
                                easing = XyMotion.EmphasizedEasing,
                            ),
                        )
                        phase = PlayerOverlayPhase.Hidden
                        dismissRequested = false
                    }
                }
            }
            val dragState =
                rememberDraggableState { dragDelta ->
                    if (!dismissRequested && phase != PlayerOverlayPhase.Exiting && isDragging) {
                        dragOffsetY =
                            updatePlayerDismissOffset(
                                currentOffsetPx = dragOffsetY,
                                dragDeltaPx = dragDelta,
                                maxOffsetPx = dismissTargetOffset,
                            )
                    }
                }
            val dragModifier =
                Modifier.draggable(
                    state = dragState,
                    orientation = Orientation.Vertical,
                    enabled = !dismissRequested && phase != PlayerOverlayPhase.Exiting,
                    startDragImmediately = offsetY.isRunning,
                    onDragStarted = {
                        dragOffsetY = offsetY.value.coerceIn(0f, dismissTargetOffset)
                        isDragging = true
                        scope.launch { offsetY.stop() }
                    },
                    onDragStopped = { releaseVelocity ->
                        if (!dismissRequested && phase != PlayerOverlayPhase.Exiting) {
                            when (
                                resolvePlayerDismissTarget(
                                    offsetPx = dragOffsetY,
                                    releaseVelocityPxPerSecond = releaseVelocity,
                                    distanceThresholdPx = dismissThreshold,
                                    velocityThresholdPxPerSecond = dismissVelocityThreshold,
                                )
                            ) {
                                PlayerDismissTarget.Dismiss -> requestDismiss()
                                PlayerDismissTarget.Restore ->
                                    scope.launch {
                                        offsetY.snapTo(dragOffsetY)
                                        isDragging = false
                                        offsetY.animateTo(
                                            targetValue = 0f,
                                            animationSpec = XyMotion.SheetDismissSpring,
                                            initialVelocity = releaseVelocity,
                                        )
                                    }
                            }
                        }
                    },
                )
            val translationY =
                when {
                    isDragging -> dragOffsetY
                    phase == PlayerOverlayPhase.Hidden -> dismissTargetOffset
                    else -> offsetY.value
                }

            Box(
                modifier =
                Modifier
                    .fillMaxSize()
                    .graphicsLayer { this.translationY = translationY }
                    .testTag(PlayerTransitionTestTags.Surface),
            ) {
                content(
                    requestDismiss,
                    dragModifier,
                    phase == PlayerOverlayPhase.Visible,
                )
            }
        }
    }
}
