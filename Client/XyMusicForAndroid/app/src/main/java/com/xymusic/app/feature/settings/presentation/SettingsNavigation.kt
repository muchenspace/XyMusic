package com.xymusic.app.feature.settings.presentation

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Logout
import androidx.compose.material.icons.outlined.AccountCircle
import androidx.compose.material.icons.outlined.MusicNote
import androidx.compose.material.icons.outlined.Palette
import androidx.compose.material.icons.outlined.Wifi
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationDrawerItem
import androidx.compose.material3.NavigationDrawerItemDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.xymusic.app.R

internal enum class SettingsPage(@StringRes val titleRes: Int, @StringRes val summaryRes: Int, val icon: ImageVector) {
    Profile(
        titleRes = R.string.settings_profile,
        summaryRes = R.string.settings_profile_summary,
        icon = Icons.Outlined.AccountCircle,
    ),
    Server(
        titleRes = R.string.settings_server,
        summaryRes = R.string.settings_server_summary,
        icon = Icons.Outlined.Wifi,
    ),
    Appearance(
        titleRes = R.string.settings_appearance,
        summaryRes = R.string.settings_appearance_summary,
        icon = Icons.Outlined.Palette,
    ),
    Playback(
        titleRes = R.string.settings_playback,
        summaryRes = R.string.settings_playback_summary,
        icon = Icons.Outlined.MusicNote,
    ),
    Account(
        titleRes = R.string.settings_account,
        summaryRes = R.string.settings_account_summary,
        icon = Icons.AutoMirrored.Outlined.Logout,
    ),
}

internal object SettingsTestTags {
    const val Root = "settings_root"
    const val LandscapeLeft = "settings_landscape_left"
    const val LandscapeRight = "settings_landscape_right"

    fun page(page: SettingsPage): String = "settings_page_${page.name.lowercase()}"
}

@Composable
internal fun SettingsRootContent(onPageSelected: (SettingsPage) -> Unit, modifier: Modifier = Modifier) {
    LazyColumn(
        modifier = modifier.fillMaxSize().testTag(SettingsTestTags.Root),
        contentPadding = PaddingValues(top = 12.dp, bottom = 48.dp),
    ) {
        SettingsPage.entries.forEachIndexed { index, page ->
            item(key = page.name) {
                SettingsActionItem(
                    icon = page.icon,
                    title = stringResource(page.titleRes),
                    summary = stringResource(page.summaryRes),
                    position =
                    when (index) {
                        0 -> SettingsRowPosition.First
                        SettingsPage.entries.lastIndex -> SettingsRowPosition.Last
                        else -> SettingsRowPosition.Middle
                    },
                    onClick = { onPageSelected(page) },
                )
            }
        }
    }
}

@Composable
internal fun SettingsLandscapeNavigation(
    selectedPage: SettingsPage,
    onPageSelected: (SettingsPage) -> Unit,
    modifier: Modifier = Modifier,
    compact: Boolean = false,
) {
    LazyColumn(
        modifier = modifier.testTag(SettingsTestTags.LandscapeLeft),
        contentPadding =
        PaddingValues(
            start = if (compact) 4.dp else 8.dp,
            top = 8.dp,
            end = if (compact) 4.dp else 8.dp,
            bottom = 16.dp,
        ),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        items(SettingsPage.entries, key = SettingsPage::name) { page ->
            NavigationDrawerItem(
                label = {
                    Text(
                        text = stringResource(page.titleRes),
                        style = MaterialTheme.typography.bodyLarge,
                    )
                },
                selected = page == selectedPage,
                onClick = { onPageSelected(page) },
                icon = {
                    Icon(
                        imageVector = page.icon,
                        contentDescription = null,
                    )
                },
                modifier =
                Modifier
                    .fillMaxWidth()
                    .testTag(SettingsTestTags.page(page)),
                colors =
                NavigationDrawerItemDefaults.colors(
                    selectedContainerColor = MaterialTheme.colorScheme.secondaryContainer,
                    selectedIconColor = MaterialTheme.colorScheme.onSecondaryContainer,
                    selectedTextColor = MaterialTheme.colorScheme.onSecondaryContainer,
                    unselectedContainerColor = MaterialTheme.colorScheme.surfaceContainerLow,
                ),
            )
        }
    }
}
