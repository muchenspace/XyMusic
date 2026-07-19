package com.xymusic.app.ui.theme

import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color

@Immutable
data class XyGradients(
    val surfaceOverlay: Brush,
    val heroScrim: Brush,
    val miniBarScrim: Brush,
    val topBarScrim: Brush,
    val sheetScrim: Brush,
    val ambientNeutral: Brush,
)

private val DarkSurfaceOverlay =
    Brush.verticalGradient(
        colors = listOf(Color(0xFF1C1C1E), Color(0xFF000000)),
    )

private val DarkHeroScrim =
    Brush.verticalGradient(
        colors = listOf(Color(0x00000000), Color(0xE6000000)),
    )

private val DarkMiniBarScrim =
    Brush.verticalGradient(
        colors = listOf(Color(0x00000000), Color(0x99000000)),
    )

private val DarkTopBarScrim =
    Brush.verticalGradient(
        colors = listOf(Color(0xE6000000), Color(0x00000000)),
    )

private val DarkSheetScrim =
    Brush.verticalGradient(
        colors = listOf(Color(0x00000000), Color(0x66000000)),
    )

private val DarkAmbientNeutral =
    Brush.radialGradient(
        colors = listOf(Color(0xFF2C2C2E), Color(0xFF000000)),
    )

val DarkGradient =
    XyGradients(
        surfaceOverlay = DarkSurfaceOverlay,
        heroScrim = DarkHeroScrim,
        miniBarScrim = DarkMiniBarScrim,
        topBarScrim = DarkTopBarScrim,
        sheetScrim = DarkSheetScrim,
        ambientNeutral = DarkAmbientNeutral,
    )

private val LightSurfaceOverlay =
    Brush.verticalGradient(
        colors = listOf(Color(0xFFFFFFFF), Color(0xFFF2F2F7)),
    )

private val LightHeroScrim =
    Brush.verticalGradient(
        colors = listOf(Color(0x00FFFFFF), Color(0xE6FFFFFF)),
    )

private val LightMiniBarScrim =
    Brush.verticalGradient(
        colors = listOf(Color(0x00FFFFFF), Color(0x99FFFFFF)),
    )

private val LightTopBarScrim =
    Brush.verticalGradient(
        colors = listOf(Color(0xE6FFFFFF), Color(0x00FFFFFF)),
    )

private val LightSheetScrim =
    Brush.verticalGradient(
        colors = listOf(Color(0x00FFFFFF), Color(0x66FFFFFF)),
    )

private val LightAmbientNeutral =
    Brush.radialGradient(
        colors = listOf(Color(0xFFE5E5EA), Color(0xFFFFFFFF)),
    )

val LightGradient =
    XyGradients(
        surfaceOverlay = LightSurfaceOverlay,
        heroScrim = LightHeroScrim,
        miniBarScrim = LightMiniBarScrim,
        topBarScrim = LightTopBarScrim,
        sheetScrim = LightSheetScrim,
        ambientNeutral = LightAmbientNeutral,
    )

private fun lightThemeGradients(
    surfaceTop: Color,
    surfaceBottom: Color,
    scrimColor: Color,
    ambientCenter: Color,
    ambientEdge: Color,
): XyGradients = XyGradients(
    surfaceOverlay =
    Brush.verticalGradient(
        colors = listOf(surfaceTop, surfaceBottom),
    ),
    heroScrim =
    Brush.verticalGradient(
        colors = listOf(scrimColor.copy(alpha = 0f), scrimColor.copy(alpha = 0.9f)),
    ),
    miniBarScrim =
    Brush.verticalGradient(
        colors = listOf(scrimColor.copy(alpha = 0f), scrimColor.copy(alpha = 0.6f)),
    ),
    topBarScrim =
    Brush.verticalGradient(
        colors = listOf(scrimColor.copy(alpha = 0.9f), scrimColor.copy(alpha = 0f)),
    ),
    sheetScrim =
    Brush.verticalGradient(
        colors = listOf(scrimColor.copy(alpha = 0f), scrimColor.copy(alpha = 0.4f)),
    ),
    ambientNeutral =
    Brush.radialGradient(
        colors = listOf(ambientCenter, ambientEdge),
    ),
)

internal val PeachPinkGradient =
    lightThemeGradients(
        surfaceTop = Color(0xFFFFF1F4),
        surfaceBottom = Color(0xFFFFF8F8),
        scrimColor = Color(0xFFFFF8F8),
        ambientCenter = Color(0xFFFFD9E2),
        ambientEdge = Color(0xFFFFF3EE),
    )

internal val OceanBlueGradient =
    lightThemeGradients(
        surfaceTop = Color(0xFFEDF8FB),
        surfaceBottom = Color(0xFFF5FAFC),
        scrimColor = Color(0xFFF5FAFC),
        ambientCenter = Color(0xFFA9EDFF),
        ambientEdge = Color(0xFFF1F9FB),
    )

internal val TwilightPurpleGradient =
    lightThemeGradients(
        surfaceTop = Color(0xFFF8F0FC),
        surfaceBottom = Color(0xFFFCF8FF),
        scrimColor = Color(0xFFFCF8FF),
        ambientCenter = Color(0xFFF1DAFF),
        ambientEdge = Color(0xFFFFF1F5),
    )

internal val LocalGradient = staticCompositionLocalOf { DarkGradient }

val MaterialTheme.gradients: XyGradients
    @Composable
    @ReadOnlyComposable
    get() = LocalGradient.current
