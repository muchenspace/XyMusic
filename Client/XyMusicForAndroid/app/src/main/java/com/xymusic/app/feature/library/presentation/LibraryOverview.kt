package com.xymusic.app.feature.library.presentation

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items as gridItems
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.PlaylistPlay
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.outlined.DownloadDone
import androidx.compose.material.icons.outlined.Favorite
import androidx.compose.material.icons.outlined.History
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import com.xymusic.app.core.ui.component.EmptyState
import com.xymusic.app.core.ui.component.MediaArtwork
import com.xymusic.app.feature.playlist.domain.model.PlaylistSummary

@Composable
internal fun LibraryOverview(
    playlists: List<PlaylistSummary>,
    onTabSelected: (LibraryTab) -> Unit,
    onPlaylistClick: (String) -> Unit,
    onCreatePlaylist: () -> Unit,
    modifier: Modifier = Modifier,
    wideLandscape: Boolean = false,
) {
    if (wideLandscape) {
        LandscapeLibraryOverview(
            playlists = playlists,
            onTabSelected = onTabSelected,
            onPlaylistClick = onPlaylistClick,
            onCreatePlaylist = onCreatePlaylist,
            modifier = modifier,
        )
        return
    }
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        contentPadding =
        PaddingValues(
            start = 20.dp,
            end = 20.dp,
            top = 8.dp,
            bottom = 32.dp,
        ),
        verticalArrangement = Arrangement.spacedBy(0.dp),
    ) {
        item(key = "library-categories") {
            Surface(
                shape = RoundedCornerShape(14.dp),
                color = MaterialTheme.colorScheme.surfaceContainerLow,
            ) {
                Column {
                    LibraryCategoryRow(
                        icon = Icons.Outlined.Favorite,
                        title = stringResource(R.string.library_favorites),
                        onClick = { onTabSelected(LibraryTab.Favorites) },
                    )
                    LibraryCategoryDivider()
                    LibraryCategoryRow(
                        icon = Icons.AutoMirrored.Outlined.PlaylistPlay,
                        title = stringResource(R.string.library_playlists),
                        onClick = { onTabSelected(LibraryTab.Playlists) },
                    )
                    LibraryCategoryDivider()
                    LibraryCategoryRow(
                        icon = Icons.Outlined.History,
                        title = stringResource(R.string.library_history),
                        onClick = { onTabSelected(LibraryTab.History) },
                    )
                    LibraryCategoryDivider()
                    LibraryCategoryRow(
                        icon = Icons.Outlined.DownloadDone,
                        title = stringResource(R.string.offline_downloads),
                        onClick = { onTabSelected(LibraryTab.Downloads) },
                    )
                }
            }
        }
        item(key = "recent-title") {
            Text(
                text = stringResource(R.string.library_recently_added),
                modifier = Modifier.padding(top = 28.dp, bottom = 12.dp),
                style = MaterialTheme.typography.headlineSmall,
                fontWeight = FontWeight.Bold,
            )
        }
        if (playlists.isEmpty()) {
            item(key = "recent-empty") {
                EmptyState(
                    title = stringResource(R.string.playlist_empty_title),
                    message = stringResource(R.string.playlist_empty_message),
                    icon = Icons.AutoMirrored.Outlined.PlaylistPlay,
                    actionLabel = stringResource(R.string.playlist_create),
                    onAction = onCreatePlaylist,
                )
            }
        } else {
            items(
                count = (playlists.size + 1) / 2,
                key = { row -> "playlist-row-$row" },
            ) { row ->
                val firstIndex = row * 2
                Row(
                    modifier = Modifier.fillMaxWidth().padding(bottom = 18.dp),
                    horizontalArrangement = Arrangement.spacedBy(14.dp),
                ) {
                    LibraryPlaylistTile(
                        playlist = playlists[firstIndex],
                        onClick = { onPlaylistClick(playlists[firstIndex].id) },
                        modifier = Modifier.weight(1f),
                    )
                    val second = playlists.getOrNull(firstIndex + 1)
                    if (second != null) {
                        LibraryPlaylistTile(
                            playlist = second,
                            onClick = { onPlaylistClick(second.id) },
                            modifier = Modifier.weight(1f),
                        )
                    } else {
                        Spacer(modifier = Modifier.weight(1f))
                    }
                }
            }
        }
    }
}

@Composable
private fun LandscapeLibraryOverview(
    playlists: List<PlaylistSummary>,
    onTabSelected: (LibraryTab) -> Unit,
    onPlaylistClick: (String) -> Unit,
    onCreatePlaylist: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val categories =
        listOf(
            Triple(Icons.Outlined.Favorite, R.string.library_favorites, LibraryTab.Favorites),
            Triple(Icons.AutoMirrored.Outlined.PlaylistPlay, R.string.library_playlists, LibraryTab.Playlists),
            Triple(Icons.Outlined.History, R.string.library_history, LibraryTab.History),
            Triple(Icons.Outlined.DownloadDone, R.string.offline_downloads, LibraryTab.Downloads),
        )
    LazyVerticalGrid(
        columns = GridCells.Adaptive(160.dp),
        modifier = modifier.fillMaxSize(),
        contentPadding = PaddingValues(start = 12.dp, end = 12.dp, bottom = 24.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        item(key = "library-categories", span = { GridItemSpan(maxLineSpan) }) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                categories.forEach { (icon, labelRes, tab) ->
                    Surface(
                        modifier =
                        Modifier
                            .weight(1f)
                            .clickable(role = Role.Button) { onTabSelected(tab) },
                        shape = RoundedCornerShape(10.dp),
                        color = MaterialTheme.colorScheme.surfaceContainerLow,
                    ) {
                        Column(modifier = Modifier.padding(horizontal = 10.dp, vertical = 9.dp)) {
                            Icon(
                                imageVector = icon,
                                contentDescription = null,
                                tint = MaterialTheme.colorScheme.primary,
                                modifier = Modifier.size(22.dp),
                            )
                            Spacer(modifier = Modifier.height(4.dp))
                            Text(
                                text = stringResource(labelRes),
                                style = MaterialTheme.typography.labelLarge,
                                maxLines = 1,
                            )
                        }
                    }
                }
            }
        }
        item(key = "recent-title", span = { GridItemSpan(maxLineSpan) }) {
            Text(
                text = stringResource(R.string.library_recently_added),
                modifier = Modifier.padding(top = 4.dp),
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.Bold,
            )
        }
        if (playlists.isEmpty()) {
            item(key = "recent-empty", span = { GridItemSpan(maxLineSpan) }) {
                EmptyState(
                    title = stringResource(R.string.playlist_empty_title),
                    message = stringResource(R.string.playlist_empty_message),
                    icon = Icons.AutoMirrored.Outlined.PlaylistPlay,
                    actionLabel = stringResource(R.string.playlist_create),
                    onAction = onCreatePlaylist,
                )
            }
        } else {
            gridItems(playlists, key = PlaylistSummary::id) { playlist ->
                LibraryPlaylistTile(
                    playlist = playlist,
                    onClick = { onPlaylistClick(playlist.id) },
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        }
    }
}

@Composable
internal fun LibraryCategoryRow(icon: ImageVector, title: String, onClick: () -> Unit) {
    Row(
        modifier =
        Modifier
            .fillMaxWidth()
            .clickable(role = Role.Button, onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 11.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Surface(
            modifier = Modifier.size(32.dp),
            shape = RoundedCornerShape(7.dp),
            color = MaterialTheme.colorScheme.primary,
            contentColor = Color.White,
        ) {
            Icon(icon, contentDescription = null, modifier = Modifier.padding(6.dp))
        }
        Spacer(modifier = Modifier.width(12.dp))
        Text(title, modifier = Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
        Icon(
            imageVector = Icons.Default.ChevronRight,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
internal fun LibraryCategoryDivider() {
    HorizontalDivider(
        modifier = Modifier.padding(start = 58.dp),
        thickness = 0.5.dp,
        color = MaterialTheme.colorScheme.outlineVariant,
    )
}

@Composable
internal fun LibraryPlaylistTile(playlist: PlaylistSummary, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Column(modifier = modifier.clickable(role = Role.Button, onClick = onClick)) {
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
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            style = MaterialTheme.typography.bodyMedium,
            fontWeight = FontWeight.Medium,
        )
        Text(
            text = stringResource(R.string.playlist_track_count, playlist.trackCount),
            maxLines = 1,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.bodySmall,
        )
    }
}
