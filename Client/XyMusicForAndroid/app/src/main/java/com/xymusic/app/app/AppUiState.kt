package com.xymusic.app.app

import androidx.compose.runtime.Immutable
import com.xymusic.app.core.network.ServerEndpoint
import com.xymusic.app.core.preferences.ThemePreference
import com.xymusic.app.core.session.AppSessionState

@Immutable
data class AppUiState(
    val sessionState: AppSessionState = AppSessionState.Loading,
    val dynamicColorEnabled: Boolean = false,
    val themePreference: ThemePreference = ThemePreference.SYSTEM,
    val serverEndpoint: ServerEndpoint? = null,
    val serverSwitchState: ServerSwitchState = ServerSwitchState.Idle,
)
