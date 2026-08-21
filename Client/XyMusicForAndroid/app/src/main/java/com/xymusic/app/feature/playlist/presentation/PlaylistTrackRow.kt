package com.xymusic.app.feature.playlist.presentation

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.snap
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.DragHandle
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.zIndex
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
    isDragging: Boolean,
    onPlay: () -> Unit,
    onDragStarted: () -> Boolean,
    onMove: (Int) -> Boolean,
    onReorderFinished: () -> Unit,
    onReorderCancelled: () -> Unit,
    onRemove: () -> Unit,
    onMore: () -> Unit,
    modifier: Modifier = Modifier,
    compact: Boolean = false,
) {
    var menuExpanded by remember { mutableStateOf(false) }
    var dragActive by remember(entry.entryId) { mutableStateOf(false) }
    var dragOffsetY by remember(entry.entryId) { mutableFloatStateOf(0f) }
    val moveUpLabel = stringResource(R.string.player_move_up)
    val moveDownLabel = stringResource(R.string.player_move_down)
    val currentIndex by rememberUpdatedState(index)
    val currentLastIndex by rememberUpdatedState(lastIndex)
    val currentOnDragStarted by rememberUpdatedState(onDragStarted)
    val currentOnMove by rememberUpdatedState(onMove)
    val currentOnReorderFinished by rememberUpdatedState(onReorderFinished)
    val currentOnReorderCancelled by rememberUpdatedState(onReorderCancelled)
    val hapticFeedback = LocalHapticFeedback.current
    val rowHeight = if (compact) 62.dp else 82.dp
    val reorderThreshold = if (compact) 31.dp else 41.dp
    val handleSize = if (compact) 38.dp else 44.dp
    val visualDragging = isDragging || dragActive
    val shape = RoundedCornerShape(if (compact) 12.dp else 16.dp)
    val containerColor by animateColorAsState(
        targetValue =
        if (visualDragging) {
            MaterialTheme.colorScheme.primaryContainer
        } else {
            MaterialTheme.colorScheme.background
        },
        animationSpec = tween(durationMillis = 120, easing = FastOutSlowInEasing),
        label = "playlist-row-container",
    )
    val primaryContentColor by animateColorAsState(
        targetValue =
        if (visualDragging) {
            MaterialTheme.colorScheme.onPrimaryContainer
        } else {
            MaterialTheme.colorScheme.onSurface
        },
        animationSpec = tween(durationMillis = 120, easing = FastOutSlowInEasing),
        label = "playlist-row-primary-content",
    )
    val secondaryContentColor by animateColorAsState(
        targetValue =
        if (visualDragging) {
            MaterialTheme.colorScheme.onPrimaryContainer.copy(alpha = 0.74f)
        } else {
            MaterialTheme.colorScheme.onSurfaceVariant
        },
        animationSpec = tween(durationMillis = 120, easing = FastOutSlowInEasing),
        label = "playlist-row-secondary-content",
    )
    val shadowElevation by animateDpAsState(
        targetValue = if (visualDragging) 10.dp else 0.dp,
        animationSpec = tween(durationMillis = 120, easing = FastOutSlowInEasing),
        label = "playlist-row-shadow",
    )
    val tonalElevation by animateDpAsState(
        targetValue = if (visualDragging) 5.dp else 0.dp,
        animationSpec = tween(durationMillis = 120, easing = FastOutSlowInEasing),
        label = "playlist-row-tonal-elevation",
    )
    val scale by animateFloatAsState(
        targetValue = if (visualDragging) 1.012f else 1f,
        animationSpec = tween(durationMillis = 120, easing = FastOutSlowInEasing),
        label = "playlist-row-scale",
    )
    val handleScale by animateFloatAsState(
        targetValue = if (visualDragging) 1.12f else 1f,
        animationSpec = tween(durationMillis = 120, easing = FastOutSlowInEasing),
        label = "playlist-handle-scale",
    )
    val renderedOffsetY by animateFloatAsState(
        targetValue = dragOffsetY,
        animationSpec =
        if (visualDragging) {
            snap()
        } else {
            spring(
                dampingRatio = Spring.DampingRatioNoBouncy,
                stiffness = Spring.StiffnessMedium,
            )
        },
        label = "playlist-row-drag-offset",
    )
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

    Surface(
        modifier =
        modifier
            .fillMaxWidth()
            .height(rowHeight)
            .zIndex(if (visualDragging) 1f else 0f)
            .padding(
                horizontal = if (compact) 4.dp else 8.dp,
                vertical = if (compact) 2.dp else 3.dp,
            ).graphicsLayer {
                translationY = renderedOffsetY
                scaleX = scale
                scaleY = scale
            },
        shape = shape,
        color = containerColor,
        contentColor = primaryContentColor,
        tonalElevation = tonalElevation,
        shadowElevation = shadowElevation,
    ) {
        Row(
            modifier =
            Modifier
                .fillMaxSize()
                .semantics { customActions = actions }
                .clickable(
                    enabled = enabled && !visualDragging,
                    onClick = onPlay,
                ).padding(start = if (compact) 12.dp else 16.dp, end = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                modifier = Modifier.width(if (compact) 32.dp else 40.dp),
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
                    color = primaryContentColor,
                    fontWeight = if (visualDragging) FontWeight.SemiBold else FontWeight.Normal,
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
                    color = primaryContentColor,
                    fontWeight = if (visualDragging) FontWeight.SemiBold else FontWeight.Normal,
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
                    color = secondaryContentColor,
                    style =
                    if (compact) {
                        MaterialTheme.typography.bodySmall
                    } else {
                        MaterialTheme.typography.bodyMedium
                    },
                )
            }
            Icon(
                imageVector = Icons.Default.DragHandle,
                contentDescription = stringResource(R.string.playlist_reorder_track),
                modifier =
                Modifier
                    .size(handleSize)
                    .testTag(PlaylistDetailTestTags.reorderHandle(entry.entryId))
                    .graphicsLayer {
                        scaleX = handleScale
                        scaleY = handleScale
                    }.then(
                        if (reorderEnabled) {
                            Modifier.pointerInput(entry.entryId, compact) {
                                var dragDistance = 0f
                                detectDragGestures(
                                    onDragStart = {
                                        if (currentOnDragStarted()) {
                                            dragActive = true
                                            dragDistance = 0f
                                            dragOffsetY = 0f
                                            hapticFeedback.performHapticFeedback(HapticFeedbackType.LongPress)
                                        }
                                    },
                                    onDragEnd = {
                                        val completed = dragActive
                                        dragActive = false
                                        dragDistance = 0f
                                        dragOffsetY = 0f
                                        if (completed) currentOnReorderFinished()
                                    },
                                    onDragCancel = {
                                        val cancelled = dragActive
                                        dragActive = false
                                        dragDistance = 0f
                                        dragOffsetY = 0f
                                        if (cancelled) currentOnReorderCancelled()
                                    },
                                ) { change, amount ->
                                    if (!dragActive) return@detectDragGestures
                                    change.consume()
                                    dragDistance += amount.y
                                    dragOffsetY += amount.y
                                    val threshold = reorderThreshold.toPx()
                                    val rowStep = rowHeight.toPx()
                                    while (abs(dragDistance) >= threshold) {
                                        val direction = if (dragDistance > 0) 1 else -1
                                        val canMove =
                                            (direction < 0 && currentIndex > 0) ||
                                                (direction > 0 && currentIndex < currentLastIndex)
                                        if (!canMove || !currentOnMove(direction)) {
                                            dragDistance = 0f
                                            dragOffsetY = dragOffsetY.coerceIn(-threshold, threshold)
                                            break
                                        }
                                        hapticFeedback.performHapticFeedback(HapticFeedbackType.TextHandleMove)
                                        dragDistance -= direction * threshold
                                        dragOffsetY -= direction * rowStep
                                    }
                                }
                            }
                        } else {
                            Modifier
                        },
                    ).padding(if (compact) 8.dp else 10.dp),
                tint =
                if (visualDragging) {
                    MaterialTheme.colorScheme.primary
                } else {
                    MaterialTheme.colorScheme.onSurfaceVariant.copy(
                        alpha = if (reorderEnabled) 1f else 0.38f,
                    )
                },
            )
            Box {
                IconButton(onClick = { menuExpanded = true }, enabled = enabled && !visualDragging) {
                    Icon(
                        Icons.Default.MoreVert,
                        contentDescription = stringResource(R.string.common_more_actions),
                        tint = secondaryContentColor,
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
}
