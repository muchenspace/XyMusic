package com.xymusic.app.ui.theme

import android.app.Activity
import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.SideEffect
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.core.view.WindowCompat
import com.xymusic.app.core.preferences.ThemePreference

val MaterialTheme.spacing: Spacing
    @Composable get() = LocalSpacing.current

@Composable
fun XyMusicTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    dynamicColor: Boolean = false,
    themePreference: ThemePreference = ThemePreference.SYSTEM,
    content: @Composable () -> Unit,
) {
    val resolvedDarkTheme = themePreference.resolveDarkTheme(darkTheme)
    val supportsDynamic = Build.VERSION.SDK_INT >= Build.VERSION_CODES.S
    val colorScheme =
        when {
            dynamicColor && supportsDynamic && themePreference.supportsDynamicColor -> {
                val context = LocalContext.current
                if (resolvedDarkTheme) {
                    dynamicDarkColorScheme(context)
                } else {
                    dynamicLightColorScheme(context)
                }
            }
            else -> resolveStaticColorScheme(themePreference, resolvedDarkTheme)
        }

    val xyColors =
        when (themePreference) {
            ThemePreference.PEACH_PINK -> PeachPinkXyColors
            ThemePreference.OCEAN_BLUE -> OceanBlueXyColors
            ThemePreference.TWILIGHT_PURPLE -> TwilightPurpleXyColors
            else -> if (resolvedDarkTheme) DarkXyColors else LightXyColors
        }
    val gradients =
        when (themePreference) {
            ThemePreference.PEACH_PINK -> PeachPinkGradient
            ThemePreference.OCEAN_BLUE -> OceanBlueGradient
            ThemePreference.TWILIGHT_PURPLE -> TwilightPurpleGradient
            else -> if (resolvedDarkTheme) DarkGradient else LightGradient
        }
    val view = LocalView.current
    if (!view.isInEditMode) {
        SideEffect {
            val window = (view.context as? Activity)?.window ?: return@SideEffect
            val insetsController = WindowCompat.getInsetsController(window, view)
            // Edge-to-edge with translucent system bars; icons adapt to theme.
            WindowCompat.setDecorFitsSystemWindows(window, false)
            insetsController.isAppearanceLightStatusBars = !resolvedDarkTheme
            insetsController.isAppearanceLightNavigationBars = !resolvedDarkTheme
        }
    }

    CompositionLocalProvider(
        LocalSpacing provides Spacing(),
        LocalElevation provides if (resolvedDarkTheme) DarkElevation else LightElevation,
        LocalGradient provides gradients,
        LocalXyColors provides xyColors,
    ) {
        MaterialTheme(
            colorScheme = colorScheme,
            typography = Typography,
            shapes = XyShapes,
            content = content,
        )
    }
}

internal fun ThemePreference.resolveDarkTheme(systemDarkTheme: Boolean): Boolean = when (this) {
    ThemePreference.SYSTEM -> systemDarkTheme
    ThemePreference.DARK -> true
    ThemePreference.LIGHT,
    ThemePreference.PEACH_PINK,
    ThemePreference.OCEAN_BLUE,
    ThemePreference.TWILIGHT_PURPLE,
    -> false
}

internal fun resolveStaticColorScheme(preference: ThemePreference, resolvedDarkTheme: Boolean): ColorScheme =
    when (preference) {
        ThemePreference.PEACH_PINK -> PeachPinkColors
        ThemePreference.OCEAN_BLUE -> OceanBlueColors
        ThemePreference.TWILIGHT_PURPLE -> TwilightPurpleColors
        else -> if (resolvedDarkTheme) DarkColors else LightColors
    }
