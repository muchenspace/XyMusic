package com.xymusic.app.feature.library.presentation

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.outlined.PlaylistPlay
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.outlined.DownloadDone
import androidx.compose.material.icons.outlined.Favorite
import androidx.compose.material.icons.outlined.History
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.xymusic.app.R
import com.xymusic.app.core.ui.layout.isWideLandscape
import com.xymusic.app.feature.playlist.domain.model.PlaylistSummary
import com.xymusic.app.feature.playlist.presentation.PlaylistEditorDialog
import com.xymusic.app.feature.playlist.presentation.labelRes
import com.xymusic.app.ui.theme.spacing

internal object LibraryTestTags {
    const val LandscapeNavigation = "library_landscape_navigation"
    const val LandscapeContent = "library_landscape_content"

    fun tab(tab: LibraryTab): String = "library_tab_${tab.name}"
}

@Composable
fun LibraryScreen(
    onTrackMore: (String) -> Unit,
    onPlaylistClick: (String) -> Unit,
    modifier: Modifier = Modifier,
    onBack: (() -> Unit)? = null,
    initialTab: LibraryTab = LibraryTab.Favorites,
    viewModel: LibraryViewModel = hiltViewModel(),
) {
    val uiState by viewModel.uiState.collectAsStateWithLifecycle()
    val downloads by viewModel.downloads.collectAsStateWithLifecycle(initialValue = emptyList())
    val snackbarHostState = remember { SnackbarHostState() }
    val resources = LocalResources.current
    val favoriteListState = rememberLazyListState()
    val playlistListState = rememberLazyListState()
    val historyListState = rememberLazyListState()
    val downloadListState = rememberLazyListState()
    var showCreateDialog by remember { mutableStateOf(false) }
    var editingPlaylist by remember { mutableStateOf<PlaylistSummary?>(null) }
    var deletingPlaylist by remember { mutableStateOf<PlaylistSummary?>(null) }
    val startInOverview = onBack == null && initialTab == LibraryTab.Favorites
    var activeTab by rememberSaveable(initialTab, startInOverview) {
        mutableStateOf<LibraryTab?>(if (startInOverview) null else initialTab)
    }

    LaunchedEffect(initialTab, startInOverview) {
        viewModel.selectTab(activeTab ?: LibraryTab.Playlists)
    }
    BackHandler(enabled = activeTab != null && onBack == null) {
        activeTab = null
        viewModel.selectTab(LibraryTab.Playlists)
    }
    LaunchedEffect(viewModel, snackbarHostState, resources) {
        viewModel.effects.collect { effect ->
            when (effect) {
                is LibraryUiEffect.ShowMessage -> {
                    snackbarHostState.showSnackbar(resources.getString(effect.messageRes))
                }
            }
        }
    }

    if (showCreateDialog) {
        PlaylistEditorDialog(
            onDismiss = { showCreateDialog = false },
            onSubmit = { name, description, visibility ->
                viewModel.createPlaylist(name, description, visibility)
                showCreateDialog = false
            },
        )
    }
    editingPlaylist?.let { playlist ->
        PlaylistEditorDialog(
            title = stringResource(R.string.playlist_edit),
            submitLabel = stringResource(R.string.common_confirm),
            initialName = playlist.name,
            initialDescription = playlist.description.orEmpty(),
            initialVisibility = playlist.visibility,
            onDismiss = { editingPlaylist = null },
            onSubmit = { name, description, visibility ->
                viewModel.updatePlaylist(playlist, name, description, visibility)
                editingPlaylist = null
            },
        )
    }
    deletingPlaylist?.let { playlist ->
        AlertDialog(
            onDismissRequest = { deletingPlaylist = null },
            title = { Text(stringResource(R.string.playlist_delete_title)) },
            text = { Text(stringResource(R.string.playlist_delete_message)) },
            confirmButton = {
                TextButton(
                    onClick = {
                        viewModel.deletePlaylist(playlist)
                        deletingPlaylist = null
                    },
                ) { Text(stringResource(R.string.playlist_delete)) }
            },
            dismissButton = {
                TextButton(onClick = { deletingPlaylist = null }) {
                    Text(stringResource(R.string.common_cancel))
                }
            },
        )
    }
    val navigateBack: (() -> Unit)? =
        when {
            activeTab != null -> {
                {
                    if (onBack != null) {
                        onBack()
                    } else {
                        activeTab = null
                        viewModel.selectTab(LibraryTab.Playlists)
                    }
                }
            }

            else -> onBack
        }

    Scaffold(
        modifier = modifier.background(MaterialTheme.colorScheme.background),
        containerColor = MaterialTheme.colorScheme.background,
        snackbarHost = { SnackbarHost(snackbarHostState) },
    ) { contentPadding ->
        BoxWithConstraints(
            modifier =
            Modifier
                .fillMaxSize()
                .padding(contentPadding),
        ) {
            val wideLandscape = isWideLandscape(maxWidth, maxHeight)
            Row(modifier = Modifier.fillMaxSize()) {
                if (wideLandscape) {
                    LandscapeLibraryNavigation(
                        activeTab = activeTab,
                        onBack = navigateBack,
                        onTabSelected = { tab ->
                            activeTab = tab
                            viewModel.selectTab(tab)
                        },
                        modifier =
                        Modifier
                            .width(224.dp)
                            .fillMaxHeight(),
                    )
                }
                Box(
                    modifier =
                    Modifier
                        .weight(1f)
                        .fillMaxHeight(),
                    contentAlignment = Alignment.TopCenter,
                ) {
                    Column(
                        modifier =
                        Modifier
                            .fillMaxHeight()
                            .widthIn(max = 900.dp)
                            .testTag(LibraryTestTags.LandscapeContent),
                    ) {
                        LibraryHeader(
                            title =
                            activeTab?.let { stringResource(it.labelRes()) }
                                ?: stringResource(R.string.library_title),
                            selectedTab = activeTab,
                            isRefreshing = uiState.isRefreshing,
                            onBack = if (wideLandscape) null else navigateBack,
                            onCreatePlaylist = { showCreateDialog = true },
                            onRefresh = viewModel::refresh,
                            compact = wideLandscape,
                        )
                        if (uiState.isRefreshing) {
                            LinearProgressIndicator(
                                modifier = Modifier.fillMaxWidth(),
                                color = MaterialTheme.colorScheme.primary,
                            )
                        }
                        when (activeTab) {
                            null ->
                                LibraryOverview(
                                    playlists = uiState.playlists,
                                    onTabSelected = { tab ->
                                        activeTab = tab
                                        viewModel.selectTab(tab)
                                    },
                                    onPlaylistClick = onPlaylistClick,
                                    onCreatePlaylist = { showCreateDialog = true },
                                    wideLandscape = wideLandscape,
                                    modifier = Modifier.weight(1f),
                                )

                            LibraryTab.Favorites ->
                                FavoriteTracks(
                                    viewModel = viewModel,
                                    onTrackMore = onTrackMore,
                                    refreshFailed = uiState.refreshFailed,
                                    onRetry = viewModel::refresh,
                                    listState = favoriteListState,
                                    modifier = Modifier.weight(1f),
                                )

                            LibraryTab.Playlists ->
                                Playlists(
                                    playlists = uiState.playlists,
                                    onPlaylistClick = onPlaylistClick,
                                    onCreatePlaylist = { showCreateDialog = true },
                                    onEditPlaylist = { editingPlaylist = it },
                                    onDeletePlaylist = { deletingPlaylist = it },
                                    enabled = !uiState.isMutating,
                                    refreshFailed = uiState.refreshFailed,
                                    onRetry = viewModel::refresh,
                                    listState = playlistListState,
                                    wideLandscape = wideLandscape,
                                    modifier = Modifier.weight(1f),
                                )

                            LibraryTab.History ->
                                History(
                                    history = viewModel.history,
                                    onPlay = viewModel::playQueue,
                                    onTrackMore = onTrackMore,
                                    refreshFailed = uiState.refreshFailed,
                                    onRetry = viewModel::refresh,
                                    listState = historyListState,
                                    modifier = Modifier.weight(1f),
                                )

                            LibraryTab.Downloads ->
                                DownloadedTracks(
                                    tracks = downloads,
                                    onPlay = viewModel::playQueue,
                                    onTrackMore = onTrackMore,
                                    listState = downloadListState,
                                    modifier = Modifier.weight(1f),
                                )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun LibraryHeader(
    title: String,
    selectedTab: LibraryTab?,
    isRefreshing: Boolean,
    onBack: (() -> Unit)?,
    onCreatePlaylist: () -> Unit,
    onRefresh: () -> Unit,
    compact: Boolean = false,
) {
    Row(
        modifier =
        Modifier
            .fillMaxWidth()
            .padding(
                start =
                if (onBack == null) {
                    MaterialTheme.spacing.contentPadding
                } else {
                    MaterialTheme.spacing.small
                },
                top = if (compact) 2.dp else MaterialTheme.spacing.compact,
                end = MaterialTheme.spacing.small,
                bottom = if (compact) 2.dp else MaterialTheme.spacing.compact,
            ),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (onBack != null) {
            IconButton(onClick = onBack) {
                Icon(
                    imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                    contentDescription = stringResource(R.string.common_back),
                )
            }
            Spacer(modifier = Modifier.width(MaterialTheme.spacing.small))
        }
        Text(
            text = title,
            modifier = Modifier.weight(1f),
            style =
            if (compact) {
                MaterialTheme.typography.headlineMedium
            } else if (onBack == null) {
                MaterialTheme.typography.displaySmall
            } else {
                MaterialTheme.typography.headlineLarge
            },
            fontWeight = FontWeight.Bold,
        )
        if (selectedTab == null || selectedTab == LibraryTab.Playlists) {
            IconButton(onClick = onCreatePlaylist) {
                Icon(
                    imageVector = Icons.Default.Add,
                    contentDescription = stringResource(R.string.playlist_create),
                    tint = MaterialTheme.colorScheme.primary,
                )
            }
        }
        if (selectedTab != LibraryTab.Downloads) {
            IconButton(onClick = onRefresh, enabled = !isRefreshing) {
                Icon(
                    imageVector = Icons.Default.Refresh,
                    contentDescription = stringResource(R.string.catalog_refresh),
                    tint = MaterialTheme.colorScheme.primary,
                )
            }
        }
    }
}

@Composable
private fun LandscapeLibraryNavigation(
    activeTab: LibraryTab?,
    onBack: (() -> Unit)?,
    onTabSelected: (LibraryTab) -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier =
        modifier
            .background(MaterialTheme.colorScheme.surfaceContainerLow)
            .padding(horizontal = 10.dp, vertical = 8.dp)
            .testTag(LibraryTestTags.LandscapeNavigation),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            if (onBack != null) {
                IconButton(onClick = onBack) {
                    Icon(
                        imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                        contentDescription = stringResource(R.string.common_back),
                    )
                }
            }
            Text(
                text = stringResource(R.string.library_title),
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.Bold,
                modifier = Modifier.padding(horizontal = 8.dp, vertical = 10.dp),
            )
        }
        LibraryTab.entries.forEach { tab ->
            val selected = activeTab == tab
            Row(
                modifier =
                Modifier
                    .fillMaxWidth()
                    .background(
                        color =
                        if (selected) {
                            MaterialTheme.colorScheme.primaryContainer
                        } else {
                            MaterialTheme.colorScheme.surfaceContainerLow
                        },
                        shape = MaterialTheme.shapes.medium,
                    ).clickable(role = Role.Tab) { onTabSelected(tab) }
                    .padding(horizontal = 12.dp, vertical = 11.dp)
                    .testTag(LibraryTestTags.tab(tab)),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(
                    imageVector = tab.icon(),
                    contentDescription = null,
                    tint =
                    if (selected) {
                        MaterialTheme.colorScheme.onPrimaryContainer
                    } else {
                        MaterialTheme.colorScheme.onSurfaceVariant
                    },
                )
                Spacer(modifier = Modifier.width(12.dp))
                Text(
                    text = stringResource(tab.labelRes()),
                    style = MaterialTheme.typography.bodyLarge,
                    fontWeight = if (selected) FontWeight.Bold else FontWeight.Medium,
                )
            }
        }
    }
}

private fun LibraryTab.icon() = when (this) {
    LibraryTab.Favorites -> Icons.Outlined.Favorite
    LibraryTab.Playlists -> Icons.AutoMirrored.Outlined.PlaylistPlay
    LibraryTab.History -> Icons.Outlined.History
    LibraryTab.Downloads -> Icons.Outlined.DownloadDone
}

internal fun LibraryTab.labelRes(): Int = when (this) {
    LibraryTab.Favorites -> R.string.library_favorites
    LibraryTab.Playlists -> R.string.library_playlists
    LibraryTab.History -> R.string.library_history
    LibraryTab.Downloads -> R.string.offline_downloads
}
