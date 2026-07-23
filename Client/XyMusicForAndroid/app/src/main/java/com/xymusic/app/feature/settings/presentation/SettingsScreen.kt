package com.xymusic.app.feature.settings.presentation

import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.xymusic.app.R
import com.xymusic.app.core.network.ServerEndpoint
import com.xymusic.app.core.ui.layout.isCompactLandscape
import com.xymusic.app.core.ui.layout.isWideLandscape
import com.xymusic.app.feature.server.presentation.ServerEndpointDialog
import com.xymusic.app.feature.settings.domain.model.UserProfile
import com.xymusic.app.ui.theme.XyMotion

private data class SettingsDialogState(
    val editProfile: Boolean,
    val profile: UserProfile?,
    val editServer: Boolean,
    val serverEndpoint: ServerEndpoint,
    val confirmReset: Boolean,
    val confirmLogout: Boolean,
    val confirmLogoutAll: Boolean,
)

private data class SettingsDialogActions(
    val onDismissProfile: () -> Unit,
    val onSaveProfile: (String, String?) -> Unit,
    val onDismissServer: () -> Unit,
    val onSaveServer: (ServerEndpoint) -> Unit,
    val onDismissReset: () -> Unit,
    val onConfirmReset: () -> Unit,
    val onDismissLogout: () -> Unit,
    val onConfirmLogout: () -> Unit,
    val onDismissLogoutAll: () -> Unit,
    val onConfirmLogoutAll: () -> Unit,
)

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(
    dynamicColorEnabled: Boolean,
    onDynamicColorChanged: (Boolean) -> Unit,
    serverEndpoint: ServerEndpoint,
    onServerEndpointChanged: (ServerEndpoint) -> Unit,
    modifier: Modifier = Modifier,
    onBack: (() -> Unit)? = null,
    viewModel: SettingsViewModel = hiltViewModel(),
) {
    val uiState by viewModel.uiState.collectAsStateWithLifecycle()
    val snackbarHostState = remember { SnackbarHostState() }
    val resources = LocalResources.current
    var selectedPage by rememberSaveable { mutableStateOf<SettingsPage?>(null) }
    var editProfile by remember { mutableStateOf(false) }
    var confirmReset by remember { mutableStateOf(false) }
    var confirmLogout by remember { mutableStateOf(false) }
    var confirmLogoutAll by remember { mutableStateOf(false) }
    var editServer by remember { mutableStateOf(false) }
    val avatarPicker =
        rememberLauncherForActivityResult(ActivityResultContracts.PickVisualMedia()) { uri ->
            uri?.let(viewModel::uploadAvatar)
        }

    LaunchedEffect(viewModel, snackbarHostState, resources) {
        viewModel.effects.collect { effect ->
            when (effect) {
                is SettingsUiEffect.ShowMessage -> {
                    snackbarHostState.showSnackbar(resources.getString(effect.messageRes))
                }
            }
        }
    }

    SettingsDialogs(
        state =
        SettingsDialogState(
            editProfile = editProfile,
            profile = uiState.profile,
            editServer = editServer,
            serverEndpoint = serverEndpoint,
            confirmReset = confirmReset,
            confirmLogout = confirmLogout,
            confirmLogoutAll = confirmLogoutAll,
        ),
        actions =
        SettingsDialogActions(
            onDismissProfile = { editProfile = false },
            onSaveProfile = { displayName, bio ->
                viewModel.updateProfile(displayName, bio)
                editProfile = false
            },
            onDismissServer = { editServer = false },
            onSaveServer = { endpoint ->
                editServer = false
                if (endpoint != serverEndpoint) onServerEndpointChanged(endpoint)
            },
            onDismissReset = { confirmReset = false },
            onConfirmReset = {
                confirmReset = false
                viewModel.resetSettings()
            },
            onDismissLogout = { confirmLogout = false },
            onConfirmLogout = {
                confirmLogout = false
                viewModel.logout()
            },
            onDismissLogoutAll = { confirmLogoutAll = false },
            onConfirmLogoutAll = {
                confirmLogoutAll = false
                viewModel.logoutAllSessions()
            },
        ),
    )

    BoxWithConstraints(modifier = modifier.fillMaxSize()) {
        val wideLandscape = isWideLandscape(maxWidth, maxHeight)
        val compactLandscape = isCompactLandscape(maxWidth, maxHeight)
        val activePage = selectedPage ?: if (wideLandscape) SettingsPage.Profile else null
        val navigateBack =
            when {
                !wideLandscape && selectedPage != null -> {
                    { selectedPage = null }
                }

                else -> onBack
            }
        val settingsActions =
            SettingsActions(
                onEditProfile = { editProfile = true },
                onAvatarClick = {
                    avatarPicker.launch(
                        PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly),
                    )
                },
                onEditServer = { editServer = true },
                onReset = { confirmReset = true },
                onLogoutAll = { confirmLogoutAll = true },
                onLogout = { confirmLogout = true },
            )

        BackHandler(enabled = !wideLandscape && selectedPage != null) {
            selectedPage = null
        }

        Scaffold(
            modifier = Modifier.fillMaxSize(),
            containerColor = MaterialTheme.colorScheme.surfaceContainerLow,
            snackbarHost = { SnackbarHost(snackbarHostState) },
            topBar = {
                TopAppBar(
                    title = {
                        Text(
                            text =
                            stringResource(
                                if (!wideLandscape && activePage != null) {
                                    activePage.titleRes
                                } else {
                                    R.string.settings_title
                                },
                            ),
                            style = MaterialTheme.typography.titleLarge,
                            fontWeight = FontWeight.SemiBold,
                        )
                    },
                    navigationIcon = {
                        if (navigateBack != null) {
                            IconButton(onClick = navigateBack) {
                                Icon(
                                    imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                                    contentDescription = stringResource(R.string.common_back),
                                )
                            }
                        }
                    },
                    actions = {
                        if (activePage == SettingsPage.Profile) {
                            IconButton(
                                onClick = viewModel::refreshProfile,
                                enabled = !uiState.isRefreshingProfile,
                            ) {
                                Icon(
                                    Icons.Default.Refresh,
                                    contentDescription =
                                    stringResource(R.string.settings_refresh_profile),
                                )
                            }
                        }
                    },
                    colors =
                    TopAppBarDefaults.topAppBarColors(
                        containerColor = MaterialTheme.colorScheme.surfaceContainerLow,
                    ),
                )
            },
        ) { contentPadding ->
            Column(
                modifier =
                Modifier
                    .fillMaxSize()
                    .padding(contentPadding),
            ) {
                if (uiState.isRefreshingProfile || uiState.isSaving) {
                    LinearProgressIndicator(
                        modifier = Modifier.fillMaxWidth(),
                        color = MaterialTheme.colorScheme.primary,
                    )
                }
                if (wideLandscape) {
                    SettingsLandscapeContent(
                        selectedPage = requireNotNull(activePage),
                        onPageSelected = { selectedPage = it },
                        uiState = uiState,
                        dynamicColorEnabled = dynamicColorEnabled,
                        onDynamicColorChanged = onDynamicColorChanged,
                        serverEndpoint = serverEndpoint,
                        viewModel = viewModel,
                        actions = settingsActions,
                        compact = compactLandscape,
                        modifier = Modifier.weight(1f),
                    )
                } else {
                    NarrowSettingsContent(
                        selectedPage = selectedPage,
                        onPageSelected = { selectedPage = it },
                        uiState = uiState,
                        dynamicColorEnabled = dynamicColorEnabled,
                        onDynamicColorChanged = onDynamicColorChanged,
                        serverEndpoint = serverEndpoint,
                        viewModel = viewModel,
                        actions = settingsActions,
                        modifier = Modifier.weight(1f),
                    )
                }
            }
        }
    }
}

@Composable
private fun NarrowSettingsContent(
    selectedPage: SettingsPage?,
    onPageSelected: (SettingsPage) -> Unit,
    uiState: SettingsUiState,
    dynamicColorEnabled: Boolean,
    onDynamicColorChanged: (Boolean) -> Unit,
    serverEndpoint: ServerEndpoint,
    viewModel: SettingsViewModel,
    actions: SettingsActions,
    modifier: Modifier = Modifier,
) {
    AnimatedContent(
        targetState = selectedPage,
        modifier = modifier,
        transitionSpec = {
            settingsPageTransition(forward = targetState != null)
        },
        label = "settings_page",
    ) { page ->
        if (page == null) {
            SettingsRootContent(
                onPageSelected = onPageSelected,
            )
        } else {
            SettingsPageContent(
                page = page,
                uiState = uiState,
                dynamicColorEnabled = dynamicColorEnabled,
                onDynamicColorChanged = onDynamicColorChanged,
                serverEndpoint = serverEndpoint,
                viewModel = viewModel,
                actions = actions,
            )
        }
    }
}

private fun settingsPageTransition(forward: Boolean) =
    (
        slideInHorizontally(
            animationSpec = tween(XyMotion.Quick, easing = XyMotion.NavigationEasing),
            initialOffsetX = { width -> if (forward) width / 12 else -width / 12 },
        ) + fadeIn(tween(XyMotion.Quick, easing = XyMotion.NavigationEasing))
    ) togetherWith (
        slideOutHorizontally(
            animationSpec = tween(XyMotion.Quick, easing = XyMotion.NavigationEasing),
            targetOffsetX = { width -> if (forward) -width / 16 else width / 16 },
        ) + fadeOut(tween(XyMotion.Quick, easing = XyMotion.NavigationEasing))
    )

@Composable
private fun SettingsLandscapeContent(
    selectedPage: SettingsPage,
    onPageSelected: (SettingsPage) -> Unit,
    uiState: SettingsUiState,
    dynamicColorEnabled: Boolean,
    onDynamicColorChanged: (Boolean) -> Unit,
    serverEndpoint: ServerEndpoint,
    viewModel: SettingsViewModel,
    actions: SettingsActions,
    compact: Boolean,
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier.fillMaxWidth(),
        contentAlignment = Alignment.TopCenter,
    ) {
        Row(
            modifier =
            Modifier
                .widthIn(max = 1_120.dp)
                .fillMaxSize()
                .padding(horizontal = if (compact) 4.dp else 12.dp),
            horizontalArrangement = Arrangement.spacedBy(if (compact) 4.dp else 12.dp),
        ) {
            SettingsLandscapeNavigation(
                selectedPage = selectedPage,
                onPageSelected = onPageSelected,
                compact = compact,
                modifier =
                Modifier
                    .width(if (compact) 208.dp else 232.dp)
                    .fillMaxHeight(),
            )
            Box(
                modifier =
                Modifier
                    .weight(1f)
                    .fillMaxHeight(),
                contentAlignment = Alignment.TopCenter,
            ) {
                SettingsPageContent(
                    page = selectedPage,
                    uiState = uiState,
                    dynamicColorEnabled = dynamicColorEnabled,
                    onDynamicColorChanged = onDynamicColorChanged,
                    serverEndpoint = serverEndpoint,
                    viewModel = viewModel,
                    actions = actions,
                    showHeading = true,
                    compact = compact,
                    modifier =
                    Modifier
                        .fillMaxHeight()
                        .widthIn(max = 760.dp)
                        .testTag(SettingsTestTags.LandscapeRight),
                )
            }
        }
    }
}

@Composable
private fun SettingsDialogs(state: SettingsDialogState, actions: SettingsDialogActions) {
    if (state.editProfile && state.profile != null) {
        EditProfileDialog(
            profile = state.profile,
            onDismiss = actions.onDismissProfile,
            onSave = actions.onSaveProfile,
        )
    }
    if (state.editServer) {
        ServerEndpointDialog(
            currentEndpoint = state.serverEndpoint,
            onDismiss = actions.onDismissServer,
            onSave = actions.onSaveServer,
        )
    }
    if (state.confirmReset) {
        ConfirmationDialog(
            title = stringResource(R.string.settings_reset_title),
            message = stringResource(R.string.settings_reset_message),
            confirmLabel = stringResource(R.string.settings_reset),
            onDismiss = actions.onDismissReset,
            onConfirm = actions.onConfirmReset,
        )
    }
    if (state.confirmLogout) {
        ConfirmationDialog(
            title = stringResource(R.string.settings_logout_title),
            message = stringResource(R.string.settings_logout_message),
            confirmLabel = stringResource(R.string.settings_logout),
            onDismiss = actions.onDismissLogout,
            onConfirm = actions.onConfirmLogout,
        )
    }
    if (state.confirmLogoutAll) {
        ConfirmationDialog(
            title = stringResource(R.string.settings_logout_all_title),
            message = stringResource(R.string.settings_logout_all_message),
            confirmLabel = stringResource(R.string.settings_logout_all),
            onDismiss = actions.onDismissLogoutAll,
            onConfirm = actions.onConfirmLogoutAll,
        )
    }
}
