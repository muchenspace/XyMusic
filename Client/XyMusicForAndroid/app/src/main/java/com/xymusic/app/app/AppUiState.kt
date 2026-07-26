package com.xymusic.app.app

import androidx.compose.runtime.Immutable
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.domain.server.ServerEndpoint
import com.xymusic.app.domain.settings.ThemePreference

@Immutable
data class AppUiState(
    val sessionState: AppSessionState = AppSessionState.Loading,
    val dynamicColorEnabled: Boolean = false,
    val themePreference: ThemePreference = ThemePreference.SYSTEM,
    val serverEndpoint: ServerEndpoint? = null,
    val serverSwitchState: ServerSwitchState = ServerSwitchState.Idle,
)
