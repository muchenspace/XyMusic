package com.xymusic.app.app

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.testTagsAsResourceId
import androidx.compose.ui.tooling.preview.Preview
import com.xymusic.app.R
import com.xymusic.app.app.navigation.AuthNavigation
import com.xymusic.app.app.navigation.MainNavigation
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.ui.component.LoadingState
import com.xymusic.app.core.ui.server.ServerSetupScreen
import com.xymusic.app.domain.server.ServerEndpoint
import com.xymusic.app.ui.theme.XyMusicTheme
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.emptyFlow

@Composable
fun XyMusicApp(
    uiState: AppUiState,
    effects: Flow<AppUiEffect>,
    onDynamicColorChanged: (Boolean) -> Unit,
    onServerEndpointChanged: (ServerEndpoint) -> Unit,
    modifier: Modifier = Modifier,
) {
    val snackbarHostState = remember { SnackbarHostState() }
    val switchFailureMessage = stringResource(R.string.server_switch_failed)
    val failedAttemptId = (uiState.serverSwitchState as? ServerSwitchState.Failed)?.attemptId
    val resources = LocalResources.current

    LaunchedEffect(effects, snackbarHostState, resources) {
        effects.collect { effect ->
            when (effect) {
                is AppUiEffect.ShowMessage ->
                    snackbarHostState.showSnackbar(resources.getString(effect.messageRes))
            }
        }
    }

    LaunchedEffect(failedAttemptId) {
        if (failedAttemptId != null) snackbarHostState.showSnackbar(switchFailureMessage)
    }

    Box(
        modifier =
        modifier
            .fillMaxSize()
            .semantics { testTagsAsResourceId = true },
    ) {
        Surface(
            modifier = Modifier.fillMaxSize(),
            color = MaterialTheme.colorScheme.background,
        ) {
            val serverEndpoint = uiState.serverEndpoint
            when {
                uiState.serverSwitchState is ServerSwitchState.Switching -> LoadingState()
                uiState.sessionState == AppSessionState.Loading && serverEndpoint == null -> LoadingState()
                serverEndpoint == null -> ServerSetupContent(onSave = onServerEndpointChanged)
                // MainActivity keeps the system splash up while the session is loading. Compose
                // the eventual auth destination behind it so the first visible auth frame is
                // not also the first composition of the form tree.
                uiState.sessionState == AppSessionState.Loading ||
                    uiState.sessionState == AppSessionState.SignedOut ->
                    AuthContent(
                        snackbarHostState = snackbarHostState,
                        serverEndpoint = serverEndpoint,
                        onServerEndpointChanged = onServerEndpointChanged,
                    )
                uiState.sessionState is AppSessionState.SignedIn ->
                    MainContent(
                        snackbarHostState = snackbarHostState,
                        dynamicColorEnabled = uiState.dynamicColorEnabled,
                        onDynamicColorChanged = onDynamicColorChanged,
                        serverEndpoint = serverEndpoint,
                        onServerEndpointChanged = onServerEndpointChanged,
                    )
            }
        }
        if (uiState.serverEndpoint == null) {
            SnackbarHost(
                hostState = snackbarHostState,
                modifier = Modifier.align(Alignment.BottomCenter),
            )
        }
    }
}

// Keep the root state switch shallow so cold-start verification only loads the selected app branch.
@Composable
private fun ServerSetupContent(onSave: (ServerEndpoint) -> Unit) {
    ServerSetupScreen(onSave = onSave)
}

@Composable
private fun AuthContent(
    snackbarHostState: SnackbarHostState,
    serverEndpoint: ServerEndpoint,
    onServerEndpointChanged: (ServerEndpoint) -> Unit,
) {
    AuthNavigation(
        snackbarHostState = snackbarHostState,
        serverEndpoint = serverEndpoint,
        onServerEndpointChanged = onServerEndpointChanged,
    )
}

@Composable
private fun MainContent(
    snackbarHostState: SnackbarHostState,
    dynamicColorEnabled: Boolean,
    onDynamicColorChanged: (Boolean) -> Unit,
    serverEndpoint: ServerEndpoint,
    onServerEndpointChanged: (ServerEndpoint) -> Unit,
) {
    MainNavigation(
        snackbarHostState = snackbarHostState,
        dynamicColorEnabled = dynamicColorEnabled,
        onDynamicColorChanged = onDynamicColorChanged,
        serverEndpoint = serverEndpoint,
        onServerEndpointChanged = onServerEndpointChanged,
    )
}

@Preview(showBackground = true)
@Composable
private fun SignedOutPreview() {
    XyMusicTheme {
        XyMusicApp(
            uiState =
            AppUiState(
                sessionState = AppSessionState.SignedOut,
                serverEndpoint = checkNotNull(ServerEndpoint.parse("localhost", "3000")),
            ),
            effects = emptyFlow(),
            onDynamicColorChanged = {},
            onServerEndpointChanged = {},
        )
    }
}

@Preview(showBackground = true)
@Composable
private fun SignedInPreview() {
    XyMusicTheme {
        XyMusicApp(
            uiState =
            AppUiState(
                sessionState = AppSessionState.SignedIn("preview"),
                serverEndpoint = checkNotNull(ServerEndpoint.parse("localhost", "3000")),
            ),
            effects = emptyFlow(),
            onDynamicColorChanged = {},
            onServerEndpointChanged = {},
        )
    }
}
