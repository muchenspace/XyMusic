package com.xymusic.app.feature.playlist.presentation

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectDragGesturesAfterLongPress
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import kotlin.math.abs

@Composable
internal fun PlaylistTrackRow(
    entry: PlaylistEntryUi,
    index: Int,
    lastIndex: Int,
    enabled: Boolean,
    removeEnabled: Boolean,
    reorderEnabled: Boolean,
    onPlay: () -> Unit,
    onMove: (Int) -> Boolean,
    onReorderFinished: () -> Unit,
    onReorderCancelled: () -> Unit,
    onRemove: () -> Unit,
    onMore: () -> Unit,
    compact: Boolean = false,
    modifier: Modifier = Modifier,
) {
    var menuExpanded by remember { mutableStateOf(false) }
    var dragDistance by remember(entry.entryId) { mutableFloatStateOf(0f) }
    val moveUpLabel = stringResource(R.string.player_move_up)
    val moveDownLabel = stringResource(R.string.player_move_down)
    val rowHeight = if (compact) 62.dp else 82.dp
    val reorderThreshold = if (compact) 36.dp else 44.dp
    val actions =
        if (reorderEnabled) {
            buildList {
                if (index > 0) {
                    add(
                        CustomAccessibilityAction(moveUpLabel) {
                            onMove(-1).also { moved -> if (moved) onReorderFinished() }
                        },
                    )
                }
                if (index < lastIndex) {
                    add(
                        CustomAccessibilityAction(moveDownLabel) {
                            onMove(1).also { moved -> if (moved) onReorderFinished() }
                        },
                    )
                }
            }
        } else {
            emptyList()
        }
    Row(
        modifier =
        modifier
            .fillMaxWidth()
            .height(rowHeight)
            .background(MaterialTheme.colorScheme.background)
            .semantics { customActions = actions }
            .pointerInput(entry.entryId, index, lastIndex, reorderEnabled) {
                if (!reorderEnabled) return@pointerInput
                detectDragGesturesAfterLongPress(
                    onDragEnd = {
                        dragDistance = 0f
                        onReorderFinished()
                    },
                    onDragCancel = {
                        dragDistance = 0f
                        onReorderCancelled()
                    },
                ) { change, amount ->
                    change.consume()
                    dragDistance += amount.y
                    if (abs(dragDistance) >= reorderThreshold.toPx()) {
                        val direction = if (dragDistance > 0) 1 else -1
                        if ((direction < 0 && index > 0) || (direction > 0 && index < lastIndex)) {
                            onMove(direction)
                        }
                        dragDistance = 0f
                    }
                }
            }.clickable(
                enabled = enabled,
                onClick = onPlay,
            )
            .padding(start = if (compact) 16.dp else 20.dp, end = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            modifier = Modifier.width(if (compact) 36.dp else 44.dp),
            contentAlignment = Alignment.CenterStart,
        ) {
            Text(
                text = (index + 1).toString(),
                style =
                if (compact) {
                    MaterialTheme.typography.titleMedium
                } else {
                    MaterialTheme.typography.titleLarge
                },
                color = MaterialTheme.colorScheme.onSurface,
            )
        }
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = entry.track.title,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                style =
                if (compact) {
                    MaterialTheme.typography.bodyLarge
                } else {
                    MaterialTheme.typography.titleLarge
                },
                fontWeight = FontWeight.Normal,
            )
            Text(
                text =
                buildString {
                    append(entry.track.artists.joinToString(" / ") { it.name })
                    entry.track.album?.title?.takeIf(String::isNotBlank)?.let {
                        append("  ·  ").append(it)
                    }
                },
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                style =
                if (compact) {
                    MaterialTheme.typography.bodySmall
                } else {
                    MaterialTheme.typography.bodyMedium
                },
            )
        }
        Box {
            IconButton(onClick = { menuExpanded = true }, enabled = enabled) {
                Icon(
                    Icons.Default.MoreVert,
                    contentDescription = stringResource(R.string.common_more_actions),
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            DropdownMenu(expanded = menuExpanded, onDismissRequest = { menuExpanded = false }) {
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.track_actions)) },
                    onClick = {
                        menuExpanded = false
                        onMore()
                    },
                    leadingIcon = { Icon(Icons.Default.MoreVert, contentDescription = null) },
                )
                DropdownMenuItem(
                    text = { Text(moveUpLabel) },
                    onClick = {
                        menuExpanded = false
                        if (onMove(-1)) onReorderFinished()
                    },
                    enabled = reorderEnabled && index > 0,
                )
                DropdownMenuItem(
                    text = { Text(moveDownLabel) },
                    onClick = {
                        menuExpanded = false
                        if (onMove(1)) onReorderFinished()
                    },
                    enabled = reorderEnabled && index < lastIndex,
                )
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.playlist_remove_track)) },
                    onClick = {
                        menuExpanded = false
                        onRemove()
                    },
                    leadingIcon = { Icon(Icons.Default.Delete, contentDescription = null) },
                    enabled = removeEnabled,
                )
            }
        }
    }
}
