package com.xymusic.app.app.trackactions

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.PlaylistAdd
import androidx.compose.material.icons.automirrored.outlined.PlaylistPlay
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.outlined.DeleteOutline
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material.icons.outlined.DownloadDone
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import com.xymusic.app.core.ui.component.MediaArtwork
import com.xymusic.app.core.ui.layout.isCompactLandscape
import com.xymusic.app.feature.playlist.domain.model.PlaylistSummary
import com.xymusic.app.feature.playlist.domain.model.PlaylistVisibility
import com.xymusic.app.feature.playlist.presentation.PlaylistEditorDialog

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TrackActionsSheet(
    uiState: TrackActionsUiState,
    onDismiss: () -> Unit,
    onToggleFavorite: () -> Unit,
    onAddToPlaylist: (PlaylistSummary) -> Unit,
    onCreatePlaylistAndAdd: (String, String?, PlaylistVisibility) -> Unit,
    onDownload: () -> Unit,
    onRemoveDownload: () -> Unit,
) {
    if (uiState.selectedTrackId == null) return
    var showCreateDialog by remember(uiState.selectedTrackId) { mutableStateOf(false) }
    val actions =
        TrackActionsCallbacks(
            onToggleFavorite = onToggleFavorite,
            onAddToPlaylist = onAddToPlaylist,
            onShowCreatePlaylist = { showCreateDialog = true },
            onDownload = onDownload,
            onRemoveDownload = onRemoveDownload,
        )

    BoxWithConstraints(modifier = Modifier.fillMaxSize()) {
        val compactLandscape = isCompactLandscape(maxWidth, maxHeight)
        val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = compactLandscape)
        val compactContentMaxHeight =
            (maxHeight - TrackActionsSheetVerticalChrome).coerceAtLeast(TrackActionsMinimumContentHeight)
        ModalBottomSheet(
            onDismissRequest = onDismiss,
            sheetState = sheetState,
            shape = RoundedCornerShape(topStart = 28.dp, topEnd = 28.dp),
        ) {
            if (compactLandscape) {
                Box(
                    modifier =
                    Modifier
                        .fillMaxWidth()
                        .windowInsetsPadding(
                            WindowInsets.safeDrawing.only(
                                WindowInsetsSides.Horizontal + WindowInsetsSides.Bottom,
                            ),
                        ),
                ) {
                    CompactLandscapeTrackActionsContent(
                        uiState = uiState,
                        actions = actions,
                        modifier =
                        Modifier
                            .align(Alignment.TopCenter)
                            .widthIn(max = TrackActionsLandscapeMaxWidth)
                            .heightIn(max = compactContentMaxHeight)
                            .fillMaxWidth(),
                    )
                }
            } else {
                PortraitTrackActionsContent(uiState = uiState, actions = actions)
            }
        }
    }

    if (showCreateDialog) {
        PlaylistEditorDialog(
            title = stringResource(R.string.playlist_create_and_add),
            submitLabel = stringResource(R.string.playlist_create_and_add),
            onDismiss = { showCreateDialog = false },
            onSubmit = { name, description, visibility ->
                showCreateDialog = false
                onCreatePlaylistAndAdd(name, description, visibility)
            },
        )
    }
}

@Composable
private fun PortraitTrackActionsContent(uiState: TrackActionsUiState, actions: TrackActionsCallbacks) {
    Column(
        modifier =
        Modifier
            .fillMaxWidth()
            .testTag(TrackActionsTestTags.PortraitContent),
    ) {
        TrackActionsProgress(uiState)
        FavoriteAction(uiState = uiState, onClick = actions.onToggleFavorite)
        DownloadAction(
            uiState = uiState,
            onDownload = actions.onDownload,
            onRemoveDownload = actions.onRemoveDownload,
        )
        HorizontalDivider()
        PlaylistSectionHeader()
        CreatePlaylistAction(uiState = uiState, onClick = actions.onShowCreatePlaylist)
        if (uiState.playlists.isEmpty()) {
            EmptyPlaylistsAction()
        } else {
            LazyColumn(modifier = Modifier.heightIn(max = 360.dp)) {
                items(uiState.playlists, key = PlaylistSummary::id) { playlist ->
                    PlaylistAction(
                        playlist = playlist,
                        enabled = !uiState.isMutating,
                        onClick = { actions.onAddToPlaylist(playlist) },
                    )
                }
            }
        }
    }
}

@Composable
private fun CompactLandscapeTrackActionsContent(
    uiState: TrackActionsUiState,
    actions: TrackActionsCallbacks,
    modifier: Modifier = Modifier,
) {
    LazyColumn(
        modifier = modifier.testTag(TrackActionsTestTags.CompactList),
        contentPadding = PaddingValues(bottom = 12.dp),
    ) {
        if (uiState.isMutating || uiState.isDownloading) {
            item(key = "track-actions-progress", contentType = "progress") {
                LinearProgressIndicator(modifier = Modifier.fillMaxWidth())
            }
        }
        item(key = "track-actions-favorite", contentType = "action") {
            FavoriteAction(uiState = uiState, onClick = actions.onToggleFavorite)
        }
        item(key = "track-actions-download", contentType = "action") {
            DownloadAction(
                uiState = uiState,
                onDownload = actions.onDownload,
                onRemoveDownload = actions.onRemoveDownload,
            )
        }
        item(key = "track-actions-divider", contentType = "divider") { HorizontalDivider() }
        item(key = "track-actions-playlist-header", contentType = "header") { PlaylistSectionHeader() }
        item(key = "track-actions-create-playlist", contentType = "action") {
            CreatePlaylistAction(uiState = uiState, onClick = actions.onShowCreatePlaylist)
        }
        if (uiState.playlists.isEmpty()) {
            item(key = "track-actions-empty-playlists", contentType = "empty") { EmptyPlaylistsAction() }
        } else {
            items(
                items = uiState.playlists,
                key = PlaylistSummary::id,
                contentType = { "playlist" },
            ) { playlist ->
                PlaylistAction(
                    playlist = playlist,
                    enabled = !uiState.isMutating,
                    onClick = { actions.onAddToPlaylist(playlist) },
                )
            }
        }
    }
}

@Composable
private fun TrackActionsProgress(uiState: TrackActionsUiState) {
    if (uiState.isMutating || uiState.isDownloading) {
        LinearProgressIndicator(modifier = Modifier.fillMaxWidth())
    }
}

@Composable
private fun FavoriteAction(uiState: TrackActionsUiState, onClick: () -> Unit) {
    ListItem(
        headlineContent = {
            Text(
                stringResource(
                    if (uiState.selectedIsFavorite) {
                        R.string.library_remove_favorite
                    } else {
                        R.string.library_add_favorite
                    },
                ),
            )
        },
        leadingContent = {
            Icon(
                imageVector =
                if (uiState.selectedIsFavorite) {
                    Icons.Default.Favorite
                } else {
                    Icons.Outlined.FavoriteBorder
                },
                contentDescription = null,
                tint =
                if (uiState.selectedIsFavorite) {
                    MaterialTheme.colorScheme.tertiary
                } else {
                    MaterialTheme.colorScheme.onSurfaceVariant
                },
            )
        },
        modifier = Modifier.clickable(enabled = !uiState.isMutating, onClick = onClick),
    )
}

@Composable
private fun DownloadAction(uiState: TrackActionsUiState, onDownload: () -> Unit, onRemoveDownload: () -> Unit) {
    ListItem(
        headlineContent = {
            Text(
                stringResource(
                    when {
                        uiState.isDownloading -> R.string.offline_downloading
                        uiState.selectedIsDownloaded -> R.string.offline_remove_download
                        else -> R.string.offline_download
                    },
                ),
            )
        },
        leadingContent = {
            Icon(
                imageVector =
                when {
                    uiState.selectedIsDownloaded -> Icons.Outlined.DeleteOutline
                    uiState.isDownloading -> Icons.Outlined.Download
                    else -> Icons.Outlined.DownloadDone
                },
                contentDescription = null,
            )
        },
        modifier =
        Modifier.clickable(
            enabled = !uiState.isMutating && !uiState.isDownloading,
            onClick = if (uiState.selectedIsDownloaded) onRemoveDownload else onDownload,
        ),
    )
}

@Composable
private fun PlaylistSectionHeader() {
    Text(
        text = stringResource(R.string.playlist_add_track),
        modifier =
        Modifier
            .fillMaxWidth()
            .padding(horizontal = 24.dp, vertical = 16.dp),
        style = MaterialTheme.typography.titleMedium,
    )
}

@Composable
private fun CreatePlaylistAction(uiState: TrackActionsUiState, onClick: () -> Unit) {
    ListItem(
        headlineContent = { Text(stringResource(R.string.playlist_create_and_add)) },
        leadingContent = { Icon(Icons.Default.Add, contentDescription = null) },
        modifier = Modifier.clickable(enabled = !uiState.isMutating, onClick = onClick),
    )
}

@Composable
private fun EmptyPlaylistsAction() {
    ListItem(
        headlineContent = { Text(stringResource(R.string.playlist_empty_title)) },
        supportingContent = { Text(stringResource(R.string.playlist_empty_message)) },
        leadingContent = { Icon(Icons.AutoMirrored.Outlined.PlaylistAdd, contentDescription = null) },
    )
}

@Composable
private fun PlaylistAction(playlist: PlaylistSummary, enabled: Boolean, onClick: () -> Unit) {
    ListItem(
        headlineContent = { Text(playlist.name, maxLines = 1) },
        supportingContent = { Text(stringResource(R.string.playlist_track_count, playlist.trackCount)) },
        leadingContent = {
            MediaArtwork(
                url = playlist.cover?.url,
                cacheKey = playlist.cover?.cacheKey,
                contentDescription = null,
                modifier = Modifier.size(48.dp),
                fallbackIcon = Icons.AutoMirrored.Outlined.PlaylistPlay,
            )
        },
        modifier =
        Modifier
            .testTag(TrackActionsTestTags.playlist(playlist.id))
            .clickable(enabled = enabled, onClick = onClick),
    )
}

private data class TrackActionsCallbacks(
    val onToggleFavorite: () -> Unit,
    val onAddToPlaylist: (PlaylistSummary) -> Unit,
    val onShowCreatePlaylist: () -> Unit,
    val onDownload: () -> Unit,
    val onRemoveDownload: () -> Unit,
)

private val TrackActionsLandscapeMaxWidth = 720.dp
private val TrackActionsSheetVerticalChrome = 80.dp
private val TrackActionsMinimumContentHeight = 120.dp

internal object TrackActionsTestTags {
    const val PortraitContent = "track_actions_portrait_content"
    const val CompactList = "track_actions_compact_list"

    fun playlist(id: String): String = "track_actions_playlist_$id"
}
