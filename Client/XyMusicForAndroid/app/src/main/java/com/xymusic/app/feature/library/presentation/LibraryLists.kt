package com.xymusic.app.feature.library.presentation

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.PlaylistPlay
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.outlined.DownloadDone
import androidx.compose.material.icons.outlined.Favorite
import androidx.compose.material.icons.outlined.History
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.paging.LoadState
import androidx.paging.PagingData
import androidx.paging.compose.collectAsLazyPagingItems
import com.xymusic.app.R
import com.xymusic.app.core.ui.component.EmptyState
import com.xymusic.app.core.ui.component.ErrorState
import com.xymusic.app.core.ui.component.MediaArtwork
import com.xymusic.app.core.ui.media.CachedCatalogBanner
import com.xymusic.app.core.ui.media.CatalogPagedList
import com.xymusic.app.core.ui.media.CatalogTrackRow
import com.xymusic.app.core.ui.media.CatalogTrackUi
import com.xymusic.app.feature.playlist.domain.model.PlaylistSummary
import com.xymusic.app.feature.playlist.presentation.labelRes
import com.xymusic.app.ui.theme.spacing
import kotlinx.coroutines.flow.Flow

@Composable
internal fun FavoriteTracks(
    viewModel: LibraryViewModel,
    onTrackMore: (String) -> Unit,
    refreshFailed: Boolean,
    onRetry: () -> Unit,
    listState: LazyListState,
    modifier: Modifier = Modifier,
) {
    val pagingItems = viewModel.favorites.collectAsLazyPagingItems()
    val loadedTracks = pagingItems.itemSnapshotList.items
    if (
        refreshFailed &&
        pagingItems.itemCount == 0 &&
        pagingItems.loadState.refresh is LoadState.NotLoading
    ) {
        ErrorState(onRetry = onRetry, modifier = modifier.fillMaxSize())
        return
    }
    CatalogPagedList(
        items = pagingItems,
        emptyTitle = stringResource(R.string.library_favorites_empty_title),
        emptyMessage = stringResource(R.string.library_favorites_empty_message),
        emptyIcon = Icons.Outlined.Favorite,
        itemKey = CatalogTrackUi::id,
        itemContent = { track ->
            CatalogTrackRow(
                track = track,
                onClick = { viewModel.playQueue(loadedTracks, track) },
                onPlayClick = { viewModel.playQueue(loadedTracks, track) },
                onMoreClick = { onTrackMore(track.id) },
            )
        },
        header =
        if (refreshFailed) {
            { CachedCatalogBanner(onRetry = onRetry) }
        } else {
            null
        },
        listState = listState,
        modifier = modifier,
    )
}

@Composable
internal fun Playlists(
    playlists: List<PlaylistSummary>,
    onPlaylistClick: (String) -> Unit,
    onCreatePlaylist: () -> Unit,
    onEditPlaylist: (PlaylistSummary) -> Unit,
    onDeletePlaylist: (PlaylistSummary) -> Unit,
    enabled: Boolean,
    refreshFailed: Boolean,
    onRetry: () -> Unit,
    listState: LazyListState,
    wideLandscape: Boolean = false,
    modifier: Modifier = Modifier,
) {
    if (playlists.isEmpty()) {
        Box(modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            if (refreshFailed) {
                ErrorState(onRetry = onRetry)
            } else {
                EmptyState(
                    title = stringResource(R.string.playlist_empty_title),
                    message = stringResource(R.string.playlist_empty_message),
                    icon = Icons.AutoMirrored.Outlined.PlaylistPlay,
                    actionLabel = stringResource(R.string.playlist_create),
                    onAction = onCreatePlaylist,
                )
            }
        }
        return
    }
    val columns = if (wideLandscape) 2 else 1
    LazyColumn(
        state = listState,
        modifier = modifier.fillMaxSize(),
        contentPadding =
        PaddingValues(
            start = MaterialTheme.spacing.contentPadding,
            end = MaterialTheme.spacing.contentPadding,
            top = MaterialTheme.spacing.small,
            bottom = 32.dp,
        ),
        verticalArrangement = Arrangement.spacedBy(MaterialTheme.spacing.small),
    ) {
        if (refreshFailed) {
            item(key = "playlist-refresh-failed") {
                CachedCatalogBanner(onRetry = onRetry)
            }
        }
        items(
            count = (playlists.size + columns - 1) / columns,
            key = { row -> "playlist-row-$row" },
        ) { row ->
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                repeat(columns) { column ->
                    val playlist = playlists.getOrNull(row * columns + column)
                    if (playlist != null) {
                        Box(modifier = Modifier.weight(1f)) {
                            PlaylistRow(
                                playlist = playlist,
                                enabled = enabled,
                                onClick = { onPlaylistClick(playlist.id) },
                                onEdit = { onEditPlaylist(playlist) },
                                onDelete = { onDeletePlaylist(playlist) },
                            )
                        }
                    } else {
                        Spacer(modifier = Modifier.weight(1f))
                    }
                }
            }
        }
    }
}

@Composable
internal fun PlaylistRow(
    playlist: PlaylistSummary,
    enabled: Boolean,
    onClick: () -> Unit,
    onEdit: () -> Unit,
    onDelete: () -> Unit,
) {
    var menuExpanded by remember { mutableStateOf(false) }
    Surface(
        modifier =
        Modifier
            .fillMaxWidth()
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick),
        shape = RoundedCornerShape(0.dp),
        color = MaterialTheme.colorScheme.surface,
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 4.dp, vertical = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            MediaArtwork(
                url = playlist.cover?.url,
                cacheKey = playlist.cover?.cacheKey,
                contentDescription = null,
                fallbackIcon = Icons.AutoMirrored.Outlined.PlaylistPlay,
                modifier = Modifier.size(62.dp),
                shape = RoundedCornerShape(8.dp),
            )
            Spacer(modifier = Modifier.width(MaterialTheme.spacing.medium))
            Column(
                modifier = Modifier.weight(1f),
                verticalArrangement = Arrangement.spacedBy(MaterialTheme.spacing.extraSmall),
            ) {
                Text(
                    text = playlist.name,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.SemiBold,
                )
                Text(
                    text =
                    stringResource(
                        R.string.playlist_summary_line,
                        playlist.trackCount,
                        stringResource(playlist.visibility.labelRes()),
                    ),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.bodyMedium,
                )
            }
            Box {
                IconButton(onClick = { menuExpanded = true }, enabled = enabled) {
                    Icon(Icons.Default.MoreVert, contentDescription = stringResource(R.string.common_more_actions))
                }
                DropdownMenu(expanded = menuExpanded, onDismissRequest = { menuExpanded = false }) {
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.playlist_edit)) },
                        onClick = {
                            menuExpanded = false
                            onEdit()
                        },
                        leadingIcon = { Icon(Icons.Default.Edit, contentDescription = null) },
                    )
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.playlist_delete)) },
                        onClick = {
                            menuExpanded = false
                            onDelete()
                        },
                        leadingIcon = { Icon(Icons.Default.Delete, contentDescription = null) },
                    )
                }
            }
        }
    }
}

@Composable
internal fun History(
    history: Flow<PagingData<LibraryHistoryUi>>,
    onPlay: (List<CatalogTrackUi>, CatalogTrackUi, Long) -> Unit,
    onTrackMore: (String) -> Unit,
    refreshFailed: Boolean,
    onRetry: () -> Unit,
    listState: LazyListState,
    modifier: Modifier = Modifier,
) {
    val pagingItems = history.collectAsLazyPagingItems()
    val loadedTracks = pagingItems.itemSnapshotList.items.map(LibraryHistoryUi::track)
    CatalogPagedList(
        items = pagingItems,
        emptyTitle = stringResource(R.string.library_history_empty_title),
        emptyMessage = stringResource(R.string.library_history_empty_message),
        emptyIcon = Icons.Outlined.History,
        itemKey = { item -> item.track.id },
        itemContent = { item ->
            CatalogTrackRow(
                track = item.track,
                onClick = { onPlay(loadedTracks, item.track, item.lastPositionMs) },
                onPlayClick = { onPlay(loadedTracks, item.track, item.lastPositionMs) },
                onMoreClick = { onTrackMore(item.track.id) },
            )
        },
        header =
        if (refreshFailed) {
            { CachedCatalogBanner(onRetry = onRetry) }
        } else {
            null
        },
        listState = listState,
        modifier = modifier,
    )
}

@Composable
internal fun DownloadedTracks(
    tracks: List<CatalogTrackUi>,
    onPlay: (List<CatalogTrackUi>, CatalogTrackUi, Long) -> Unit,
    onTrackMore: (String) -> Unit,
    listState: LazyListState,
    modifier: Modifier = Modifier,
) {
    if (tracks.isEmpty()) {
        Box(modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            EmptyState(
                title = stringResource(R.string.offline_empty_title),
                message = stringResource(R.string.offline_empty_message),
                icon = Icons.Outlined.DownloadDone,
            )
        }
        return
    }
    LazyColumn(
        state = listState,
        modifier = modifier.fillMaxSize(),
        contentPadding =
        PaddingValues(
            start = MaterialTheme.spacing.contentPadding,
            end = MaterialTheme.spacing.contentPadding,
            top = MaterialTheme.spacing.small,
            bottom = 32.dp,
        ),
        verticalArrangement = Arrangement.spacedBy(MaterialTheme.spacing.extraSmall),
    ) {
        items(tracks, key = CatalogTrackUi::id) { track ->
            CatalogTrackRow(
                track = track,
                onClick = { onPlay(tracks, track, 0L) },
                onPlayClick = { onPlay(tracks, track, 0L) },
                onMoreClick = { onTrackMore(track.id) },
            )
        }
    }
}
