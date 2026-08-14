package com.xymusic.app.ui.theme

import androidx.compose.animation.AnimatedContentTransitionScope
import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.Easing
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.IntOffset

object XyMotion {
    const val Instant = 0
    const val Fast = 100
    const val Quick = 180
    const val Standard = 260
    const val Emphasized = 340
    const val Slow = 460

    val EaseOutQuart: Easing = CubicBezierEasing(0.25f, 1f, 0.5f, 1f)
    val EaseInOutQuart: Easing = CubicBezierEasing(0.76f, 0f, 0.24f, 1f)
    val EaseOutBack: Easing = CubicBezierEasing(0.34f, 1.56f, 0.64f, 1f)
    val EaseOutExp: Easing = CubicBezierEasing(0.16f, 1f, 0.3f, 1f)
    val EmphasizedEasing: Easing = CubicBezierEasing(0.2f, 0f, 0f, 1f)
    val EmphasizedDecel: Easing = CubicBezierEasing(0.05f, 0.7f, 0.1f, 1f)
    val NavigationEasing: Easing = CubicBezierEasing(0.4f, 0f, 0.2f, 1f)

    val InteractiveSpring =
        spring<Float>(
            dampingRatio = Spring.DampingRatioMediumBouncy,
            stiffness = Spring.StiffnessLow,
        )

    val SnapSpring =
        spring<Float>(
            dampingRatio = Spring.DampingRatioNoBouncy,
            stiffness = Spring.StiffnessMedium,
        )

    val PillNavSpring =
        spring<Dp>(
            dampingRatio = Spring.DampingRatioMediumBouncy,
            stiffness = Spring.StiffnessMedium,
        )

    val PressSpring =
        spring<Float>(
            dampingRatio = Spring.DampingRatioMediumBouncy,
            stiffness = Spring.StiffnessHigh,
        )

    val SharedElementSpring =
        spring<IntOffset>(
            dampingRatio = Spring.DampingRatioLowBouncy,
            stiffness = Spring.StiffnessMediumLow,
        )

    fun snapTo() = tween<Float>(durationMillis = Emphasized, easing = EaseOutExp)

    fun fadeIn(duration: Int = Quick) = tween<Float>(durationMillis = duration, easing = EaseOutQuart)

    fun slideIn(duration: Int = Emphasized) = tween<Float>(durationMillis = duration, easing = EaseOutExp)

    val ShimmerSpec = tween<Float>(durationMillis = 1400, easing = EaseInOutQuart)

    const val SharedElementArtworkKey = "player-artwork"
    const val SharedElementMiniBarKey = "mini-bar"
}

// Route transitions stay alpha-only so navigation never remeasures the active page per frame.
fun AnimatedContentTransitionScope<*>.fadeInto() = fadeIn(
    animationSpec = tween(XyMotion.Quick, easing = XyMotion.NavigationEasing),
)

fun AnimatedContentTransitionScope<*>.fadeOutOf() = fadeOut(
    animationSpec = tween(XyMotion.Quick, easing = XyMotion.NavigationEasing),
)

fun AnimatedContentTransitionScope<*>.fadeBackInto() = fadeIn(
    animationSpec = tween(XyMotion.Quick, easing = XyMotion.NavigationEasing),
)

fun AnimatedContentTransitionScope<*>.fadeBackOutOf() = fadeOut(
    animationSpec = tween(XyMotion.Quick, easing = XyMotion.NavigationEasing),
)

fun AnimatedContentTransitionScope<*>.playerSlideInto() =
    slideIntoContainer(
        towards = AnimatedContentTransitionScope.SlideDirection.Up,
        animationSpec = tween(XyMotion.Emphasized, easing = XyMotion.EmphasizedDecel),
    ) + fadeIn(
        animationSpec = tween(XyMotion.Quick, easing = XyMotion.EmphasizedDecel),
    )

fun AnimatedContentTransitionScope<*>.playerSlideOutOf() =
    slideOutOfContainer(
        towards = AnimatedContentTransitionScope.SlideDirection.Down,
        animationSpec = tween(XyMotion.Standard, easing = XyMotion.EmphasizedEasing),
    ) + fadeOut(
        animationSpec = tween(XyMotion.Standard, easing = XyMotion.EmphasizedEasing),
    )

fun AnimatedContentTransitionScope<*>.playerFadeInto() = playerSlideInto()

fun AnimatedContentTransitionScope<*>.playerFadeOutOf() = playerSlideOutOf()

