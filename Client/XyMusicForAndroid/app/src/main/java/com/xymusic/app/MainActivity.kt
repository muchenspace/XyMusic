package com.xymusic.app

import android.os.Bundle
import android.util.Log
import androidx.activity.ComponentActivity
import androidx.activity.compose.ReportDrawnWhen
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.runtime.getValue
import androidx.core.splashscreen.SplashScreen.Companion.installSplashScreen
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.xymusic.app.app.AppViewModel
import com.xymusic.app.app.XyMusicApp
import com.xymusic.app.core.performance.FrameJankMonitor
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.ui.theme.XyMusicTheme
import com.xymusic.app.ui.theme.resolveDarkTheme
import dagger.hilt.android.AndroidEntryPoint

@AndroidEntryPoint
class MainActivity : ComponentActivity() {
    private val appViewModel: AppViewModel by viewModels()
    private var frameJankMonitor: FrameJankMonitor? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        val splashScreen = installSplashScreen()
        super.onCreate(savedInstanceState)
        if (BuildConfig.DEBUG) {
            @Suppress("DEPRECATION")
            val refreshRate = windowManager.defaultDisplay.refreshRate.coerceAtLeast(1f)
            frameJankMonitor =
                FrameJankMonitor(
                    slowFrameThresholdNanos = (NANOS_PER_SECOND / refreshRate * SLOW_FRAME_COUNT).toLong(),
                ) { durationNanos ->
                    Log.w("XyMusicJank", "slowFrameNs=$durationNanos refreshRate=$refreshRate")
                }
        }
        splashScreen.setKeepOnScreenCondition {
            appViewModel.uiState.value.sessionState == AppSessionState.Loading
        }
        enableEdgeToEdge()
        setContent {
            val uiState by appViewModel.uiState.collectAsStateWithLifecycle()
            ReportDrawnWhen { uiState.sessionState != AppSessionState.Loading }
            val systemDarkTheme = isSystemInDarkTheme()
            val darkTheme = uiState.themePreference.resolveDarkTheme(systemDarkTheme)
            XyMusicTheme(
                darkTheme = darkTheme,
                themePreference = uiState.themePreference,
                dynamicColor = uiState.dynamicColorEnabled,
            ) {
                XyMusicApp(
                    uiState = uiState,
                    effects = appViewModel.effects,
                    onDynamicColorChanged = appViewModel::setDynamicColorEnabled,
                    onServerEndpointChanged = appViewModel::setServerEndpoint,
                )
            }
        }
    }

    override fun onResume() {
        super.onResume()
        frameJankMonitor?.start()
    }

    override fun onPause() {
        frameJankMonitor?.stop()
        super.onPause()
    }

    private companion object {
        const val NANOS_PER_SECOND = 1_000_000_000f
        const val SLOW_FRAME_COUNT = 2f
    }
}
