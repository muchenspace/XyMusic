package com.xymusic.app.feature.settings.presentation

import android.os.Build
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Logout
import androidx.compose.material.icons.outlined.Cached
import androidx.compose.material.icons.outlined.HighQuality
import androidx.compose.material.icons.outlined.MusicNote
import androidx.compose.material.icons.outlined.Palette
import androidx.compose.material.icons.outlined.Wifi
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import com.xymusic.app.core.network.ServerEndpoint
import com.xymusic.app.core.preferences.MobileDataPolicy
import com.xymusic.app.core.preferences.StreamingQuality
import com.xymusic.app.core.preferences.ThemePreference

internal data class SettingsActions(
    val onEditProfile: () -> Unit,
    val onAvatarClick: () -> Unit,
    val onEditServer: () -> Unit,
    val onReset: () -> Unit,
    val onLogoutAll: () -> Unit,
    val onLogout: () -> Unit,
)

@Composable
internal fun SettingsPageContent(
    page: SettingsPage,
    uiState: SettingsUiState,
    dynamicColorEnabled: Boolean,
    onDynamicColorChanged: (Boolean) -> Unit,
    serverEndpoint: ServerEndpoint,
    viewModel: SettingsViewModel,
    actions: SettingsActions,
    modifier: Modifier = Modifier,
    showHeading: Boolean = false,
    compact: Boolean = false,
) {
    when (page) {
        SettingsPage.Profile ->
            ProfileSettingsPage(uiState, actions, modifier, showHeading, compact)

        SettingsPage.Server ->
            ServerSettingsPage(serverEndpoint, actions, modifier, showHeading, compact)

        SettingsPage.Appearance ->
            AppearanceSettingsPage(
                uiState = uiState,
                dynamicColorEnabled = dynamicColorEnabled,
                onDynamicColorChanged = onDynamicColorChanged,
                viewModel = viewModel,
                modifier = modifier,
                showHeading = showHeading,
                compact = compact,
            )

        SettingsPage.Playback ->
            PlaybackSettingsPage(uiState, viewModel, modifier, showHeading, compact)

        SettingsPage.Account ->
            AccountSettingsPage(actions, modifier, showHeading, compact)
    }
}

@Composable
private fun ProfileSettingsPage(
    uiState: SettingsUiState,
    actions: SettingsActions,
    modifier: Modifier,
    showHeading: Boolean,
    compact: Boolean,
) {
    SettingsPageList(SettingsPage.Profile, modifier, showHeading, compact) {
        item(key = "profile") {
            ProfileSection(
                profile = uiState.profile,
                onEdit = actions.onEditProfile,
                onAvatarClick = actions.onAvatarClick,
            )
        }
    }
}

@Composable
private fun ServerSettingsPage(
    serverEndpoint: ServerEndpoint,
    actions: SettingsActions,
    modifier: Modifier,
    showHeading: Boolean,
    compact: Boolean,
) {
    SettingsPageList(SettingsPage.Server, modifier, showHeading, compact) {
        item(key = "server-endpoint") {
            SettingsActionItem(
                icon = Icons.Outlined.Wifi,
                title = stringResource(R.string.settings_server_endpoint),
                summary = serverEndpoint.displayValue,
                position = SettingsRowPosition.Single,
                onClick = actions.onEditServer,
            )
        }
    }
}

@Composable
private fun AppearanceSettingsPage(
    uiState: SettingsUiState,
    dynamicColorEnabled: Boolean,
    onDynamicColorChanged: (Boolean) -> Unit,
    viewModel: SettingsViewModel,
    modifier: Modifier,
    showHeading: Boolean,
    compact: Boolean,
) {
    SettingsPageList(SettingsPage.Appearance, modifier, showHeading, compact) {
        item(key = "theme") {
            OptionRow(
                icon = Icons.Outlined.Palette,
                title = stringResource(R.string.settings_theme),
                selected = uiState.settings.theme,
                options = ThemePreference.entries,
                optionLabel = { stringResource(it.labelRes()) },
                onSelected = viewModel::setTheme,
                position = SettingsRowPosition.First,
            )
        }
        item(key = "dynamic-color") {
            val dynamicColorAvailable =
                Build.VERSION.SDK_INT >= Build.VERSION_CODES.S &&
                    uiState.settings.theme.supportsDynamicColor
            SettingsToggleItem(
                icon = Icons.Outlined.Palette,
                title = stringResource(R.string.settings_dynamic_color),
                summary =
                stringResource(
                    if (uiState.settings.theme.supportsDynamicColor) {
                        R.string.settings_dynamic_color_summary
                    } else {
                        R.string.settings_dynamic_color_fixed_theme_summary
                    },
                ),
                checked = dynamicColorEnabled,
                enabled = dynamicColorAvailable,
                onCheckedChange = onDynamicColorChanged,
                position = SettingsRowPosition.Last,
            )
        }
    }
}

@Composable
private fun PlaybackSettingsPage(
    uiState: SettingsUiState,
    viewModel: SettingsViewModel,
    modifier: Modifier,
    showHeading: Boolean,
    compact: Boolean,
) {
    SettingsPageList(SettingsPage.Playback, modifier, showHeading, compact) {
        item(key = "quality") {
            OptionRow(
                icon = Icons.Outlined.HighQuality,
                title = stringResource(R.string.settings_streaming_quality),
                selected = uiState.settings.streamingQuality,
                options = StreamingQuality.entries,
                optionLabel = { stringResource(it.labelRes()) },
                onSelected = viewModel::setStreamingQuality,
                position = SettingsRowPosition.First,
            )
        }
        item(key = "wifi-only") {
            SettingsToggleItem(
                icon = Icons.Outlined.Wifi,
                title = stringResource(R.string.settings_wifi_only),
                summary = stringResource(R.string.settings_wifi_only_summary),
                checked = uiState.settings.mobileDataPolicy == MobileDataPolicy.WIFI_ONLY,
                onCheckedChange = viewModel::setWifiOnly,
                position = SettingsRowPosition.Middle,
            )
        }
        item(key = "cache") {
            CacheLimitRow(
                valueMiB = uiState.settings.cacheLimitMiB,
                onValueChanged = viewModel::setCacheLimitMiB,
                position = SettingsRowPosition.Middle,
            )
        }
        item(key = "word-by-word-lyrics") {
            SettingsToggleItem(
                icon = Icons.Outlined.MusicNote,
                title = stringResource(R.string.settings_word_by_word_lyrics),
                summary = stringResource(R.string.settings_word_by_word_lyrics_summary),
                checked = uiState.settings.wordByWordLyricsEnabled,
                onCheckedChange = viewModel::setWordByWordLyricsEnabled,
                position = SettingsRowPosition.Last,
            )
        }
    }
}

@Composable
private fun AccountSettingsPage(actions: SettingsActions, modifier: Modifier, showHeading: Boolean, compact: Boolean) {
    SettingsPageList(SettingsPage.Account, modifier, showHeading, compact) {
        item(key = "reset") {
            SettingsActionItem(
                icon = Icons.Outlined.Cached,
                title = stringResource(R.string.settings_reset),
                summary = stringResource(R.string.settings_reset_summary),
                position = SettingsRowPosition.First,
                onClick = actions.onReset,
            )
        }
        item(key = "logout-all") {
            SettingsActionItem(
                icon = Icons.AutoMirrored.Outlined.Logout,
                title = stringResource(R.string.settings_logout_all),
                summary = stringResource(R.string.settings_logout_all_summary),
                position = SettingsRowPosition.Middle,
                onClick = actions.onLogoutAll,
            )
        }
        item(key = "logout") {
            SettingsActionItem(
                icon = Icons.AutoMirrored.Outlined.Logout,
                title = stringResource(R.string.settings_logout),
                titleColor = MaterialTheme.colorScheme.error,
                iconTint = MaterialTheme.colorScheme.error,
                position = SettingsRowPosition.Last,
                showChevron = false,
                onClick = actions.onLogout,
            )
        }
    }
}

@Composable
private fun SettingsPageList(
    page: SettingsPage,
    modifier: Modifier,
    showHeading: Boolean,
    compact: Boolean,
    content: LazyListScope.() -> Unit,
) {
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        contentPadding =
        PaddingValues(
            top = if (showHeading) 0.dp else 8.dp,
            bottom = if (compact) 16.dp else 48.dp,
        ),
    ) {
        if (showHeading) {
            item(key = "${page.name}-heading") {
                SettingsHeading(stringResource(page.titleRes))
            }
        }
        content()
    }
}
