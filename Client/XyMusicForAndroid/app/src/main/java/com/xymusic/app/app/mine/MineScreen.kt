package com.xymusic.app.app.mine

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items as gridItems
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.PlaylistPlay
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.outlined.AccountCircle
import androidx.compose.material.icons.outlined.DownloadDone
import androidx.compose.material.icons.outlined.Favorite
import androidx.compose.material.icons.outlined.History
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.xymusic.app.R
import com.xymusic.app.core.ui.component.MediaArtwork
import com.xymusic.app.core.ui.layout.isWideLandscape
import com.xymusic.app.feature.library.presentation.LibraryTab
import com.xymusic.app.feature.library.presentation.LibraryUiState
import com.xymusic.app.feature.playlist.domain.model.PlaylistSummary
import com.xymusic.app.feature.playlist.presentation.PlaylistEditorDialog
import com.xymusic.app.feature.settings.domain.model.UserProfile
import com.xymusic.app.feature.settings.presentation.SettingsUiEffect
import com.xymusic.app.feature.settings.presentation.SettingsUiState
import com.xymusic.app.feature.settings.presentation.SettingsViewModel

internal object MineTestTags {
    const val Root = "mine_root"
    const val Settings = "mine_settings"
    const val Favorites = "mine_favorites"
    const val Playlists = "mine_playlists"
    const val History = "mine_history"
    const val Downloads = "mine_downloads"
    const val CreatePlaylist = "mine_create_playlist"
    const val LandscapeAccountPane = "mine_landscape_account_pane"
    const val LandscapePlaylistPane = "mine_landscape_playlist_pane"

    fun playlist(id: String) = "mine_playlist_$id"
}

@Composable
fun MineScreen(
    onPlaylistClick: (String) -> Unit,
    onOpenLibrary: (LibraryTab) -> Unit,
    onOpenSettings: () -> Unit,
    modifier: Modifier = Modifier,
    settingsViewModel: SettingsViewModel = hiltViewModel(),
    mineViewModel: MineViewModel = hiltViewModel(),
) {
    val settingsUiState by settingsViewModel.uiState.collectAsStateWithLifecycle()
    val libraryUiState by mineViewModel.uiState.collectAsStateWithLifecycle()
    val snackbarHostState = remember { SnackbarHostState() }
    val resources = LocalResources.current
    var showCreatePlaylist by remember { mutableStateOf(false) }

    LaunchedEffect(mineViewModel, snackbarHostState, resources) {
        mineViewModel.effects.collect { effect ->
            when (effect) {
                is MineUiEffect.ShowMessage ->
                    snackbarHostState.showSnackbar(
                        resources.getString(effect.messageRes),
                    )
            }
        }
    }
    LaunchedEffect(settingsViewModel, snackbarHostState, resources) {
        settingsViewModel.effects.collect { effect ->
            when (effect) {
                is SettingsUiEffect.ShowMessage ->
                    snackbarHostState.showSnackbar(
                        resources.getString(effect.messageRes),
                    )
            }
        }
    }
    if (showCreatePlaylist) {
        PlaylistEditorDialog(
            onDismiss = { showCreatePlaylist = false },
            onSubmit = { name, description, visibility ->
                mineViewModel.createPlaylist(name, description, visibility)
                showCreatePlaylist = false
            },
        )
    }

    Scaffold(
        modifier = modifier.background(MaterialTheme.colorScheme.surface),
        containerColor = MaterialTheme.colorScheme.surface,
        snackbarHost = { SnackbarHost(snackbarHostState) },
    ) { contentPadding ->
        MineContent(
            settingsUiState = settingsUiState,
            libraryUiState = libraryUiState,
            onPlaylistClick = onPlaylistClick,
            onOpenLibrary = onOpenLibrary,
            onOpenSettings = onOpenSettings,
            onCreatePlaylist = { showCreatePlaylist = true },
            modifier = Modifier.padding(contentPadding),
        )
    }
}

@Composable
internal fun MineContent(
    settingsUiState: SettingsUiState,
    libraryUiState: LibraryUiState,
    onPlaylistClick: (String) -> Unit,
    onOpenLibrary: (LibraryTab) -> Unit,
    onOpenSettings: () -> Unit,
    onCreatePlaylist: () -> Unit,
    modifier: Modifier = Modifier,
) {
    BoxWithConstraints(modifier = modifier.fillMaxSize()) {
        if (isWideLandscape(maxWidth, maxHeight)) {
            LandscapeMineContent(
                settingsUiState = settingsUiState,
                libraryUiState = libraryUiState,
                onPlaylistClick = onPlaylistClick,
                onOpenLibrary = onOpenLibrary,
                onOpenSettings = onOpenSettings,
                onCreatePlaylist = onCreatePlaylist,
                modifier = Modifier.fillMaxSize(),
            )
        } else {
            PortraitMineContent(
                settingsUiState = settingsUiState,
                libraryUiState = libraryUiState,
                onPlaylistClick = onPlaylistClick,
                onOpenLibrary = onOpenLibrary,
                onOpenSettings = onOpenSettings,
                onCreatePlaylist = onCreatePlaylist,
                modifier = Modifier.fillMaxSize(),
            )
        }
    }
}

@Composable
private fun PortraitMineContent(
    settingsUiState: SettingsUiState,
    libraryUiState: LibraryUiState,
    onPlaylistClick: (String) -> Unit,
    onOpenLibrary: (LibraryTab) -> Unit,
    onOpenSettings: () -> Unit,
    onCreatePlaylist: () -> Unit,
    modifier: Modifier = Modifier,
) {
    LazyColumn(
        modifier =
        modifier
            .background(MaterialTheme.colorScheme.surface)
            .testTag(MineTestTags.Root),
        contentPadding = PaddingValues(bottom = 32.dp),
    ) {
        item(key = "account-header") {
            AccountHeader(onOpenSettings = onOpenSettings)
        }
        item(key = "profile") {
            AccountProfile(settingsUiState.profile)
        }
        item(key = "library-title") {
            MineSectionTitle(stringResource(R.string.library_title))
        }
        item(key = "library-links") {
            LibraryLinks(onOpenLibrary = onOpenLibrary)
        }
        item(key = "playlist-header") {
            PlaylistHeader(
                onOpenLibrary = { onOpenLibrary(LibraryTab.Playlists) },
                onCreatePlaylist = onCreatePlaylist,
                count = libraryUiState.playlists.size,
            )
        }
        if (libraryUiState.isRefreshing) {
            item(key = "refreshing") {
                LinearProgressIndicator(
                    modifier = Modifier.fillMaxWidth(),
                    color = MaterialTheme.colorScheme.primary,
                )
            }
        }
        if (libraryUiState.playlists.isEmpty()) {
            item(key = "playlist-empty") {
                EmptyPlaylists(onCreatePlaylist)
            }
        } else {
            item(key = "playlist-row") {
                LazyRow(
                    contentPadding = PaddingValues(horizontal = 20.dp),
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    items(libraryUiState.playlists, key = PlaylistSummary::id) { playlist ->
                        PlaylistTile(
                            playlist = playlist,
                            onClick = { onPlaylistClick(playlist.id) },
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun LandscapeMineContent(
    settingsUiState: SettingsUiState,
    libraryUiState: LibraryUiState,
    onPlaylistClick: (String) -> Unit,
    onOpenLibrary: (LibraryTab) -> Unit,
    onOpenSettings: () -> Unit,
    onCreatePlaylist: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier =
        modifier
            .background(MaterialTheme.colorScheme.surface)
            .padding(horizontal = 12.dp)
            .testTag(MineTestTags.Root),
        horizontalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        LazyColumn(
            modifier =
            Modifier
                .weight(0.82f)
                .fillMaxHeight()
                .testTag(MineTestTags.LandscapeAccountPane),
            contentPadding = PaddingValues(bottom = 16.dp),
        ) {
            item(key = "account-header") {
                AccountHeader(onOpenSettings = onOpenSettings, compact = true)
            }
            item(key = "profile") {
                AccountProfile(settingsUiState.profile, compact = true)
            }
            item(key = "library-title") {
                MineSectionTitle(stringResource(R.string.library_title), compact = true)
            }
            item(key = "library-links") {
                LandscapeLibraryLinks(onOpenLibrary = onOpenLibrary)
            }
        }
        LazyVerticalGrid(
            columns = GridCells.Adaptive(132.dp),
            modifier =
            Modifier
                .weight(1.18f)
                .fillMaxHeight()
                .testTag(MineTestTags.LandscapePlaylistPane),
            contentPadding = PaddingValues(start = 4.dp, end = 8.dp, bottom = 16.dp),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item(key = "playlist-header", span = { GridItemSpan(maxLineSpan) }) {
                PlaylistHeader(
                    onOpenLibrary = { onOpenLibrary(LibraryTab.Playlists) },
                    onCreatePlaylist = onCreatePlaylist,
                    count = libraryUiState.playlists.size,
                    compact = true,
                )
            }
            if (libraryUiState.isRefreshing) {
                item(key = "refreshing", span = { GridItemSpan(maxLineSpan) }) {
                    LinearProgressIndicator(
                        modifier = Modifier.fillMaxWidth(),
                        color = MaterialTheme.colorScheme.primary,
                    )
                }
            }
            if (libraryUiState.playlists.isEmpty()) {
                item(key = "playlist-empty", span = { GridItemSpan(maxLineSpan) }) {
                    EmptyPlaylists(onCreatePlaylist)
                }
            } else {
                gridItems(libraryUiState.playlists, key = PlaylistSummary::id) { playlist ->
                    PlaylistTile(
                        playlist = playlist,
                        onClick = { onPlaylistClick(playlist.id) },
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }
        }
    }
}

@Composable
private fun AccountHeader(onOpenSettings: () -> Unit, compact: Boolean = false) {
    Row(
        modifier =
        Modifier
            .fillMaxWidth()
            .padding(
                start = if (compact) 8.dp else 20.dp,
                end = if (compact) 4.dp else 12.dp,
                top = if (compact) 4.dp else 16.dp,
                bottom = if (compact) 0.dp else 8.dp,
            ),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = stringResource(R.string.navigation_mine),
            modifier = Modifier.weight(1f),
            style = if (compact) MaterialTheme.typography.headlineMedium else MaterialTheme.typography.headlineLarge,
            fontWeight = FontWeight.Bold,
        )
        IconButton(
            onClick = onOpenSettings,
            modifier = Modifier.size(44.dp).testTag(MineTestTags.Settings),
        ) {
            Icon(
                imageVector = Icons.Default.Settings,
                contentDescription = stringResource(R.string.settings_title),
                tint = MaterialTheme.colorScheme.primary,
            )
        }
    }
}

@Composable
private fun AccountProfile(profile: UserProfile?, compact: Boolean = false) {
    Row(
        modifier =
        Modifier
            .fillMaxWidth()
            .padding(horizontal = if (compact) 8.dp else 20.dp, vertical = if (compact) 6.dp else 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        MediaArtwork(
            url = profile?.avatar?.url,
            cacheKey = profile?.avatar?.cacheKey,
            contentDescription = null,
            fallbackIcon = Icons.Outlined.AccountCircle,
            shape = CircleShape,
            modifier = Modifier.size(if (compact) 58.dp else 76.dp),
        )
        Spacer(modifier = Modifier.width(14.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = profile?.displayName ?: stringResource(R.string.settings_profile_loading),
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.Bold,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            profile?.let {
                Text(
                    text = "@${it.username}",
                    color = MaterialTheme.colorScheme.primary,
                    style = MaterialTheme.typography.bodyMedium,
                )
            }
            profile?.bio?.takeIf(String::isNotBlank)?.let { bio ->
                Spacer(modifier = Modifier.height(5.dp))
                Text(
                    text = bio,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.bodyMedium,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

@Composable
private fun MineSectionTitle(title: String, compact: Boolean = false) {
    Text(
        text = title,
        modifier =
        Modifier.padding(
            start = if (compact) 8.dp else 20.dp,
            end = if (compact) 8.dp else 20.dp,
            top = if (compact) 8.dp else 20.dp,
            bottom = if (compact) 6.dp else 8.dp,
        ),
        style = if (compact) MaterialTheme.typography.titleMedium else MaterialTheme.typography.titleLarge,
        fontWeight = FontWeight.Bold,
    )
}

@Composable
private fun LibraryLinks(onOpenLibrary: (LibraryTab) -> Unit) {
    Surface(
        modifier = Modifier.padding(horizontal = 20.dp),
        shape = RoundedCornerShape(8.dp),
        color = MaterialTheme.colorScheme.surfaceContainerLow,
    ) {
        Column {
            AccountLinkRow(
                icon = Icons.Outlined.Favorite,
                label = stringResource(R.string.library_favorites),
                testTag = MineTestTags.Favorites,
                onClick = { onOpenLibrary(LibraryTab.Favorites) },
            )
            AccountDivider()
            AccountLinkRow(
                icon = Icons.AutoMirrored.Outlined.PlaylistPlay,
                label = stringResource(R.string.library_playlists),
                testTag = MineTestTags.Playlists,
                onClick = { onOpenLibrary(LibraryTab.Playlists) },
            )
            AccountDivider()
            AccountLinkRow(
                icon = Icons.Outlined.History,
                label = stringResource(R.string.library_history),
                testTag = MineTestTags.History,
                onClick = { onOpenLibrary(LibraryTab.History) },
            )
            AccountDivider()
            AccountLinkRow(
                icon = Icons.Outlined.DownloadDone,
                label = stringResource(R.string.offline_downloads),
                testTag = MineTestTags.Downloads,
                onClick = { onOpenLibrary(LibraryTab.Downloads) },
            )
        }
    }
}

@Composable
private fun LandscapeLibraryLinks(onOpenLibrary: (LibraryTab) -> Unit) {
    Column(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            LandscapeLibraryLink(
                icon = Icons.Outlined.Favorite,
                label = stringResource(R.string.library_favorites),
                testTag = MineTestTags.Favorites,
                onClick = { onOpenLibrary(LibraryTab.Favorites) },
                modifier = Modifier.weight(1f),
            )
            LandscapeLibraryLink(
                icon = Icons.AutoMirrored.Outlined.PlaylistPlay,
                label = stringResource(R.string.library_playlists),
                testTag = MineTestTags.Playlists,
                onClick = { onOpenLibrary(LibraryTab.Playlists) },
                modifier = Modifier.weight(1f),
            )
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            LandscapeLibraryLink(
                icon = Icons.Outlined.History,
                label = stringResource(R.string.library_history),
                testTag = MineTestTags.History,
                onClick = { onOpenLibrary(LibraryTab.History) },
                modifier = Modifier.weight(1f),
            )
            LandscapeLibraryLink(
                icon = Icons.Outlined.DownloadDone,
                label = stringResource(R.string.offline_downloads),
                testTag = MineTestTags.Downloads,
                onClick = { onOpenLibrary(LibraryTab.Downloads) },
                modifier = Modifier.weight(1f),
            )
        }
    }
}

@Composable
private fun LandscapeLibraryLink(
    icon: ImageVector,
    label: String,
    testTag: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Surface(
        modifier =
        modifier
            .clickable(role = Role.Button, onClick = onClick)
            .testTag(testTag),
        shape = RoundedCornerShape(8.dp),
        color = MaterialTheme.colorScheme.surfaceContainerLow,
    ) {
        Column(
            modifier = Modifier.padding(horizontal = 10.dp, vertical = 9.dp),
            horizontalAlignment = Alignment.Start,
        ) {
            Icon(
                imageVector = icon,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.primary,
                modifier = Modifier.size(21.dp),
            )
            Spacer(modifier = Modifier.height(4.dp))
            Text(
                text = label,
                style = MaterialTheme.typography.labelLarge,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

@Composable
private fun AccountLinkRow(icon: ImageVector, label: String, testTag: String, onClick: () -> Unit) {
    Row(
        modifier =
        Modifier
            .fillMaxWidth()
            .clickable(role = Role.Button, onClick = onClick)
            .testTag(testTag)
            .padding(horizontal = 14.dp, vertical = 9.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            modifier =
            Modifier
                .size(32.dp)
                .clip(RoundedCornerShape(7.dp))
                .background(MaterialTheme.colorScheme.surfaceContainerHighest),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                icon,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.size(20.dp),
            )
        }
        Spacer(modifier = Modifier.width(12.dp))
        Text(text = label, modifier = Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
        Icon(
            imageVector = Icons.Default.ChevronRight,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun AccountDivider() {
    HorizontalDivider(
        modifier = Modifier.padding(start = 58.dp),
        thickness = 0.5.dp,
        color = MaterialTheme.colorScheme.outlineVariant,
    )
}

@Composable
private fun PlaylistHeader(
    onOpenLibrary: () -> Unit,
    onCreatePlaylist: () -> Unit,
    count: Int,
    compact: Boolean = false,
) {
    Row(
        modifier =
        Modifier
            .fillMaxWidth()
            .padding(
                start = if (compact) 4.dp else 20.dp,
                end = if (compact) 0.dp else 8.dp,
                top = if (compact) 4.dp else 22.dp,
                bottom = if (compact) 4.dp else 8.dp,
            ),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = stringResource(R.string.library_playlists),
            style = if (compact) MaterialTheme.typography.titleMedium else MaterialTheme.typography.titleLarge,
            fontWeight = FontWeight.Bold,
        )
        Text(
            text = "  $count",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(modifier = Modifier.weight(1f))
        IconButton(
            onClick = onCreatePlaylist,
            modifier = Modifier.size(44.dp).testTag(MineTestTags.CreatePlaylist),
        ) {
            Icon(
                Icons.Default.Add,
                contentDescription = stringResource(R.string.playlist_create),
                tint = MaterialTheme.colorScheme.primary,
            )
        }
        TextButton(onClick = onOpenLibrary) {
            Text(
                stringResource(R.string.search_view_all),
                color = MaterialTheme.colorScheme.primary,
            )
        }
    }
}

@Composable
private fun PlaylistTile(playlist: PlaylistSummary, onClick: () -> Unit, modifier: Modifier = Modifier.width(164.dp)) {
    Column(
        modifier =
        modifier
            .clickable(role = Role.Button, onClick = onClick)
            .testTag(MineTestTags.playlist(playlist.id)),
    ) {
        MediaArtwork(
            url = playlist.cover?.url,
            cacheKey = playlist.cover?.cacheKey,
            contentDescription = null,
            fallbackIcon = Icons.AutoMirrored.Outlined.PlaylistPlay,
            shape = RoundedCornerShape(9.dp),
            modifier = Modifier.fillMaxWidth().aspectRatio(1f),
        )
        Spacer(modifier = Modifier.height(6.dp))
        Text(
            text = playlist.name,
            style = MaterialTheme.typography.bodyMedium,
            fontWeight = FontWeight.Medium,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            text = stringResource(R.string.playlist_track_count, playlist.trackCount),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 1,
        )
    }
}

@Composable
private fun EmptyPlaylists(onCreatePlaylist: () -> Unit) {
    Column(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 18.dp),
        horizontalAlignment = Alignment.Start,
    ) {
        Text(
            text = stringResource(R.string.playlist_empty_title),
            style = MaterialTheme.typography.titleMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(modifier = Modifier.height(4.dp))
        Text(
            text = stringResource(R.string.playlist_empty_message),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        TextButton(onClick = onCreatePlaylist, contentPadding = PaddingValues(0.dp)) {
            Icon(
                Icons.Default.Add,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.primary,
            )
            Spacer(Modifier.width(4.dp))
            Text(
                stringResource(R.string.playlist_create),
                color = MaterialTheme.colorScheme.primary,
            )
        }
    }
}
