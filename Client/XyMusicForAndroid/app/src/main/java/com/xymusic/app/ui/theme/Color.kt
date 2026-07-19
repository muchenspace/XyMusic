package com.xymusic.app.ui.theme

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color

val Primary = Color(0xFF2C2C2E)
val PrimaryLight = Color(0xFF48484A)
val PrimaryDark = Color(0xFF1C1C1E)
val OnPrimary = Color(0xFFFFFFFF)

val Accent = Color(0xFF2C2C2E)
val AccentLight = Color(0xFF48484A)
val AccentDark = Color(0xFF1C1C1E)
val OnAccent = Color(0xFFFFFFFF)

val Background = Color(0xFFFFFFFF)
val BackgroundElevated = Color(0xFFFFFFFF)

val Surface = Color(0xFFFFFFFF)
val SurfaceVariant = Color(0xFFF2F2F7)
val SurfaceHigh = Color(0xFFE5E5EA)
val SurfaceHighest = Color(0xFFD1D1D6)

val OnBackground = Color(0xFF000000)
val OnSurface = Color(0xFF000000)
val OnSurfaceVariant = Color(0xFF636366)
val OnSurfaceDim = Color(0xFF8E8E93)

val Outline = Color(0xFF8E8E93)
val OutlineVariant = Color(0xFFC6C6C8)

val Error = Color(0xFFFF3B30)
val OnError = Color(0xFFFFFFFF)
val ErrorContainer = Color(0xFFFFDAD4)
val OnErrorContainer = Color(0xFF410001)

val Secondary = Color(0xFF636366)
val SecondaryContainer = Color(0xFFF2F2F7)
val OnSecondary = Color(0xFFFFFFFF)
val OnSecondaryContainer = Color(0xFF1C1C1E)

val Tertiary = Color(0xFF8E8E93)
val TertiaryContainer = Color(0xFFE5E5EA)
val OnTertiary = Color(0xFFFFFFFF)
val OnTertiaryContainer = Color(0xFF1C1C1E)

val GradientStart = Color(0xFFE5E5EA)
val GradientCenter = Color(0xFFF7F7FA)
val GradientEnd = Color(0xFFFFFFFF)

val NowPlayingBg = Color(0xFFFFFFFF)
val MiniBarBg = Color(0xFFFFFFFF)

val ShimmerBase = Color(0xFFE5E5EA)
val ShimmerHighlight = Color(0xFFF7F7FA)

val DarkColors =
    darkColorScheme(
        primary = Color(0xFFAEAEB2),
        onPrimary = Color(0xFF1C1C1E),
        primaryContainer = Color(0xFF2C2C2E),
        onPrimaryContainer = Color(0xFFF2F2F7),
        inversePrimary = Color(0xFF3A3A3C),
        secondary = Color(0xFF8E8E93),
        onSecondary = Color(0xFF000000),
        secondaryContainer = Color(0xFF1C1C1E),
        onSecondaryContainer = Color(0xFFE5E5EA),
        tertiary = Color(0xFFAEAEB2),
        onTertiary = Color(0xFFFFFFFF),
        tertiaryContainer = Color(0xFF2C2C2E),
        onTertiaryContainer = Color(0xFFE5E5EA),
        background = Color(0xFF000000),
        onBackground = Color(0xFFFFFFFF),
        surface = Color(0xFF000000),
        onSurface = Color(0xFFFFFFFF),
        surfaceVariant = Color(0xFF1C1C1E),
        onSurfaceVariant = Color(0xFFAEAEB2),
        surfaceTint = Color(0xFF636366),
        inverseSurface = Color(0xFFF2F2F7),
        inverseOnSurface = Color(0xFF1C1C1E),
        surfaceContainerLowest = Color(0xFF000000),
        surfaceContainerLow = Color(0xFF0C0C0E),
        surfaceContainer = Color(0xFF1C1C1E),
        surfaceContainerHigh = Color(0xFF2C2C2E),
        surfaceContainerHighest = Color(0xFF3A3A3C),
        outline = Color(0xFF636366),
        outlineVariant = Color(0xFF38383A),
        error = Color(0xFFFF453A),
        onError = Color(0xFFFFFFFF),
        errorContainer = Color(0xFF5D1B1B),
        onErrorContainer = Color(0xFFFFB4AB),
        scrim = Color(0xFF000000),
    )

val LightColors =
    lightColorScheme(
        primary = Color(0xFF2C2C2E),
        onPrimary = Color(0xFFFFFFFF),
        primaryContainer = Color(0xFFE5E5EA),
        onPrimaryContainer = Color(0xFF1C1C1E),
        inversePrimary = Color(0xFFD1D1D6),
        secondary = Color(0xFF636366),
        onSecondary = Color(0xFFFFFFFF),
        secondaryContainer = Color(0xFFF2F2F7),
        onSecondaryContainer = Color(0xFF1C1C1E),
        tertiary = Color(0xFF636366),
        onTertiary = Color(0xFFFFFFFF),
        tertiaryContainer = Color(0xFFE5E5EA),
        onTertiaryContainer = Color(0xFF1C1C1E),
        background = Color(0xFFFFFFFF),
        onBackground = Color(0xFF000000),
        surface = Color(0xFFFFFFFF),
        onSurface = Color(0xFF000000),
        surfaceVariant = Color(0xFFF2F2F7),
        onSurfaceVariant = Color(0xFF636366),
        surfaceTint = Color(0xFF8E8E93),
        inverseSurface = Color(0xFF1C1C1C),
        inverseOnSurface = Color(0xFFF2F2F7),
        surfaceContainerLowest = Color(0xFFFFFFFF),
        surfaceContainerLow = Color(0xFFFAFAFC),
        surfaceContainer = Color(0xFFF2F2F7),
        surfaceContainerHigh = Color(0xFFE5E5EA),
        surfaceContainerHighest = Color(0xFFD1D1D6),
        outline = Color(0xFF8E8E93),
        outlineVariant = Color(0xFFC6C6C8),
        error = Color(0xFFFF3B30),
        onError = Color(0xFFFFFFFF),
        errorContainer = Color(0xFFFFDAD4),
        onErrorContainer = Color(0xFF410001),
        scrim = Color(0xFF000000),
    )

/** Warm, low-contrast surfaces with a strong rose accent. */
internal val PeachPinkColors =
    lightColorScheme(
        primary = Color(0xFFAD3158),
        onPrimary = Color(0xFFFFFFFF),
        primaryContainer = Color(0xFFFFD9E2),
        onPrimaryContainer = Color(0xFF3F0019),
        inversePrimary = Color(0xFFFFB1C5),
        secondary = Color(0xFF76565E),
        onSecondary = Color(0xFFFFFFFF),
        secondaryContainer = Color(0xFFFFD9E2),
        onSecondaryContainer = Color(0xFF2B151B),
        tertiary = Color(0xFF80552E),
        onTertiary = Color(0xFFFFFFFF),
        tertiaryContainer = Color(0xFFFFDCC0),
        onTertiaryContainer = Color(0xFF2E1500),
        background = Color(0xFFFFF8F8),
        onBackground = Color(0xFF211A1C),
        surface = Color(0xFFFFF8F8),
        onSurface = Color(0xFF211A1C),
        surfaceVariant = Color(0xFFF4DDE2),
        onSurfaceVariant = Color(0xFF524348),
        surfaceTint = Color(0xFFAD3158),
        inverseSurface = Color(0xFF362F31),
        inverseOnSurface = Color(0xFFFCEEF1),
        surfaceContainerLowest = Color(0xFFFFFFFF),
        surfaceContainerLow = Color(0xFFFFF0F3),
        surfaceContainer = Color(0xFFFBEAEC),
        surfaceContainerHigh = Color(0xFFF5E4E7),
        surfaceContainerHighest = Color(0xFFEFDEE1),
        outline = Color(0xFF857378),
        outlineVariant = Color(0xFFD7C2C7),
        error = Color(0xFFBA1A1A),
        onError = Color(0xFFFFFFFF),
        errorContainer = Color(0xFFFFDAD6),
        onErrorContainer = Color(0xFF410002),
        scrim = Color(0xFF000000),
    )

/** Cool cyan-blue surfaces inspired by clear coastal water. */
internal val OceanBlueColors =
    lightColorScheme(
        primary = Color(0xFF00677A),
        onPrimary = Color(0xFFFFFFFF),
        primaryContainer = Color(0xFFA9EDFF),
        onPrimaryContainer = Color(0xFF001F26),
        inversePrimary = Color(0xFF55D6F2),
        secondary = Color(0xFF4A6269),
        onSecondary = Color(0xFFFFFFFF),
        secondaryContainer = Color(0xFFCDE7EE),
        onSecondaryContainer = Color(0xFF051F25),
        tertiary = Color(0xFF555D7E),
        onTertiary = Color(0xFFFFFFFF),
        tertiaryContainer = Color(0xFFDCE1FF),
        onTertiaryContainer = Color(0xFF111A37),
        background = Color(0xFFF5FAFC),
        onBackground = Color(0xFF171D1F),
        surface = Color(0xFFF5FAFC),
        onSurface = Color(0xFF171D1F),
        surfaceVariant = Color(0xFFDBE4E7),
        onSurfaceVariant = Color(0xFF3F484B),
        surfaceTint = Color(0xFF00677A),
        inverseSurface = Color(0xFF2C3133),
        inverseOnSurface = Color(0xFFEDF1F3),
        surfaceContainerLowest = Color(0xFFFFFFFF),
        surfaceContainerLow = Color(0xFFEFF5F7),
        surfaceContainer = Color(0xFFE9EFF1),
        surfaceContainerHigh = Color(0xFFE3E9EB),
        surfaceContainerHighest = Color(0xFFDDE3E5),
        outline = Color(0xFF6F797C),
        outlineVariant = Color(0xFFBFC8CB),
        error = Color(0xFFBA1A1A),
        onError = Color(0xFFFFFFFF),
        errorContainer = Color(0xFFFFDAD6),
        onErrorContainer = Color(0xFF410002),
        scrim = Color(0xFF000000),
    )

/** Violet surfaces balanced with muted plum and rose accents. */
internal val TwilightPurpleColors =
    lightColorScheme(
        primary = Color(0xFF6C4B82),
        onPrimary = Color(0xFFFFFFFF),
        primaryContainer = Color(0xFFF1DAFF),
        onPrimaryContainer = Color(0xFF26003A),
        inversePrimary = Color(0xFFDDB7F6),
        secondary = Color(0xFF68586D),
        onSecondary = Color(0xFFFFFFFF),
        secondaryContainer = Color(0xFFF0DBF3),
        onSecondaryContainer = Color(0xFF231728),
        tertiary = Color(0xFF805158),
        onTertiary = Color(0xFFFFFFFF),
        tertiaryContainer = Color(0xFFFFD9DD),
        onTertiaryContainer = Color(0xFF321017),
        background = Color(0xFFFCF8FF),
        onBackground = Color(0xFF1D1B20),
        surface = Color(0xFFFCF8FF),
        onSurface = Color(0xFF1D1B20),
        surfaceVariant = Color(0xFFE9DFEA),
        onSurfaceVariant = Color(0xFF4B454D),
        surfaceTint = Color(0xFF6C4B82),
        inverseSurface = Color(0xFF322F34),
        inverseOnSurface = Color(0xFFF5EFF5),
        surfaceContainerLowest = Color(0xFFFFFFFF),
        surfaceContainerLow = Color(0xFFF7F2F9),
        surfaceContainer = Color(0xFFF1ECF3),
        surfaceContainerHigh = Color(0xFFEBE6ED),
        surfaceContainerHighest = Color(0xFFE5E1E7),
        outline = Color(0xFF7C747E),
        outlineVariant = Color(0xFFCDC4CE),
        error = Color(0xFFBA1A1A),
        onError = Color(0xFFFFFFFF),
        errorContainer = Color(0xFFFFDAD6),
        onErrorContainer = Color(0xFF410002),
        scrim = Color(0xFF000000),
    )

data class XyColors(
    val surfaceHigh: Color,
    val surfaceHighest: Color,
    val onSurfaceDim: Color,
    val onSurfaceVariant: Color,
    val gradientStart: Color,
    val gradientEnd: Color,
    val accent: Color,
    val tertiary: Color,
    val shimmerBase: Color,
    val shimmerHighlight: Color,
    val nowPlayingBg: Color,
    val miniBarBg: Color,
)

internal val DarkXyColors =
    XyColors(
        surfaceHigh = Color(0xFF2C2C2E),
        surfaceHighest = Color(0xFF3A3A3C),
        onSurfaceDim = Color(0xFF8E8E93),
        onSurfaceVariant = Color(0xFFAEAEB2),
        gradientStart = Color(0xFF1C1C1E),
        gradientEnd = Color(0xFF000000),
        accent = Color(0xFFAEAEB2),
        tertiary = Color(0xFFAEAEB2),
        shimmerBase = Color(0xFF1C1C1E),
        shimmerHighlight = Color(0xFF3A3A3C),
        nowPlayingBg = Color(0xFF000000),
        miniBarBg = Color(0xFF1C1C1E),
    )

internal val LightXyColors =
    XyColors(
        surfaceHigh = Color(0xFFE5E5EA),
        surfaceHighest = Color(0xFFD1D1D6),
        onSurfaceDim = Color(0xFF8E8E93),
        onSurfaceVariant = Color(0xFF636366),
        gradientStart = Color(0xFFE5E5EA),
        gradientEnd = Color(0xFFFFFFFF),
        accent = Color(0xFF2C2C2E),
        tertiary = Color(0xFF636366),
        shimmerBase = Color(0xFFE5E5EA),
        shimmerHighlight = Color(0xFFF7F7FA),
        nowPlayingBg = Color(0xFFFFFFFF),
        miniBarBg = Color(0xFFFFFFFF),
    )

internal val PeachPinkXyColors =
    XyColors(
        surfaceHigh = Color(0xFFF5E4E7),
        surfaceHighest = Color(0xFFEFDEE1),
        onSurfaceDim = Color(0xFF9A858B),
        onSurfaceVariant = Color(0xFF6D5960),
        gradientStart = Color(0xFFFFD9E2),
        gradientEnd = Color(0xFFFFF3EE),
        accent = Color(0xFFAD3158),
        tertiary = Color(0xFF80552E),
        shimmerBase = Color(0xFFF1DEE3),
        shimmerHighlight = Color(0xFFFFF4F6),
        nowPlayingBg = Color(0xFFFFF0F3),
        miniBarBg = Color(0xFFFFF8F8),
    )

internal val OceanBlueXyColors =
    XyColors(
        surfaceHigh = Color(0xFFE3E9EB),
        surfaceHighest = Color(0xFFDDE3E5),
        onSurfaceDim = Color(0xFF899497),
        onSurfaceVariant = Color(0xFF536166),
        gradientStart = Color(0xFFA9EDFF),
        gradientEnd = Color(0xFFF1F9FB),
        accent = Color(0xFF00677A),
        tertiary = Color(0xFF555D7E),
        shimmerBase = Color(0xFFDDE8EB),
        shimmerHighlight = Color(0xFFF3FBFD),
        nowPlayingBg = Color(0xFFEAF7FA),
        miniBarBg = Color(0xFFF5FAFC),
    )

internal val TwilightPurpleXyColors =
    XyColors(
        surfaceHigh = Color(0xFFEBE6ED),
        surfaceHighest = Color(0xFFE5E1E7),
        onSurfaceDim = Color(0xFF938B96),
        onSurfaceVariant = Color(0xFF615866),
        gradientStart = Color(0xFFF1DAFF),
        gradientEnd = Color(0xFFFFF1F5),
        accent = Color(0xFF6C4B82),
        tertiary = Color(0xFF805158),
        shimmerBase = Color(0xFFE8DFEA),
        shimmerHighlight = Color(0xFFFAF3FC),
        nowPlayingBg = Color(0xFFF7EDFC),
        miniBarBg = Color(0xFFFCF8FF),
    )

val LocalXyColors = staticCompositionLocalOf { LightXyColors }

val MaterialTheme.xyColors: XyColors
    @Composable
    @ReadOnlyComposable
    get() = LocalXyColors.current
