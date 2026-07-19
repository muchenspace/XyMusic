package com.xymusic.app.feature.playlist.presentation

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.xymusic.app.R

@Composable
internal fun PlaylistTopBar(
    title: String,
    collapsed: Boolean,
    refreshing: Boolean,
    menuExpanded: Boolean,
    menuEnabled: Boolean,
    onBack: () -> Unit,
    onRefresh: () -> Unit,
    onMenuExpandedChange: (Boolean) -> Unit,
    onEdit: () -> Unit,
    onDelete: () -> Unit,
) {
    val contentColor = if (collapsed) MaterialTheme.colorScheme.onSurface else Color.White
    Row(
        modifier =
        Modifier
            .fillMaxWidth()
            .background(if (collapsed) MaterialTheme.colorScheme.background else Color.Transparent)
            .windowInsetsPadding(WindowInsets.statusBars)
            .height(64.dp)
            .padding(horizontal = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        IconButton(onClick = onBack) {
            Icon(
                Icons.AutoMirrored.Filled.ArrowBack,
                contentDescription = stringResource(R.string.common_back),
                tint = contentColor,
                modifier = Modifier.size(30.dp),
            )
        }
        Text(
            text = if (collapsed) title else "",
            modifier = Modifier.weight(1f),
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            style = MaterialTheme.typography.titleLarge,
            fontWeight = FontWeight.SemiBold,
            color = contentColor,
        )
        IconButton(onClick = onRefresh, enabled = !refreshing) {
            if (refreshing) {
                CircularProgressIndicator(
                    modifier = Modifier.size(20.dp),
                    strokeWidth = 2.dp,
                    color = contentColor,
                )
            } else {
                Icon(
                    Icons.Default.Refresh,
                    contentDescription = stringResource(R.string.catalog_refresh),
                    tint = contentColor,
                    modifier = Modifier.size(26.dp),
                )
            }
        }
        Box {
            IconButton(
                onClick = { onMenuExpandedChange(true) },
                enabled = menuEnabled,
            ) {
                Icon(
                    Icons.Default.MoreVert,
                    contentDescription = stringResource(R.string.common_more_actions),
                    tint = contentColor,
                    modifier = Modifier.size(28.dp),
                )
            }
            DropdownMenu(
                expanded = menuExpanded,
                onDismissRequest = { onMenuExpandedChange(false) },
            ) {
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.playlist_edit)) },
                    onClick = {
                        onMenuExpandedChange(false)
                        onEdit()
                    },
                    leadingIcon = { Icon(Icons.Default.Edit, contentDescription = null) },
                )
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.playlist_delete)) },
                    onClick = {
                        onMenuExpandedChange(false)
                        onDelete()
                    },
                    leadingIcon = { Icon(Icons.Default.Delete, contentDescription = null) },
                )
            }
        }
    }
}

@Composable
internal fun PlaylistTrackToolbar(
    trackCount: Int,
    refreshing: Boolean,
    onPlayAll: () -> Unit,
    onRefresh: () -> Unit,
    onEdit: () -> Unit,
    onMore: () -> Unit,
    playAllEnabled: Boolean,
    compact: Boolean = false,
) {
    Row(
        modifier =
        Modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.background)
            .padding(
                start = if (compact) 16.dp else 24.dp,
                end = 8.dp,
                top = if (compact) 2.dp else 10.dp,
                bottom = if (compact) 2.dp else 10.dp,
            ),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = stringResource(R.string.playlist_track_count, trackCount),
            modifier = Modifier.weight(1f),
            style =
            if (compact) {
                MaterialTheme.typography.titleMedium
            } else {
                MaterialTheme.typography.titleLarge
            },
            fontWeight = FontWeight.Medium,
        )
        IconButton(onClick = onPlayAll, enabled = playAllEnabled) {
            Icon(
                Icons.Default.PlayArrow,
                contentDescription = stringResource(R.string.playlist_play_all),
                modifier = Modifier.size(27.dp),
            )
        }
        IconButton(onClick = onEdit) {
            Icon(
                Icons.Default.Edit,
                contentDescription = stringResource(R.string.playlist_edit),
                modifier = Modifier.size(24.dp),
            )
        }
        IconButton(onClick = onRefresh, enabled = !refreshing) {
            Icon(
                Icons.Default.Refresh,
                contentDescription = stringResource(R.string.catalog_refresh),
                modifier = Modifier.size(25.dp),
            )
        }
        IconButton(onClick = onMore) {
            Icon(
                Icons.Default.MoreVert,
                contentDescription = stringResource(R.string.common_more_actions),
                modifier = Modifier.size(27.dp),
            )
        }
    }
}
