package com.xymusic.app.feature.player.presentation

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectDragGesturesAfterLongPress
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.DragHandle
import androidx.compose.material.icons.filled.GraphicEq
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.outlined.DeleteOutline
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import com.xymusic.app.core.ui.component.MediaArtwork
import com.xymusic.app.core.ui.layout.isCompactLandscape
import com.xymusic.app.core.ui.layout.isWideLandscape
import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
import com.xymusic.app.feature.player.domain.model.RepeatMode
import kotlin.math.abs

@Composable
internal fun QueueContent(
    queue: List<PlayerQueueItem>,
    currentQueueItemId: String?,
    shuffleEnabled: Boolean,
    repeatMode: RepeatMode,
    onCyclePlaybackMode: () -> Unit,
    onSelect: (String) -> Unit,
    onRemove: (String) -> Unit,
    onMove: (String, Int) -> Unit,
    onClear: () -> Unit,
    modifier: Modifier = Modifier,
) {
    BoxWithConstraints(
        modifier = modifier.fillMaxSize().testTag(PlayerTestTags.QueueContent),
        contentAlignment = Alignment.TopCenter,
    ) {
        val wideLandscape = isWideLandscape(maxWidth, maxHeight)
        val compactLandscape = isCompactLandscape(maxWidth, maxHeight)
        Column(
            modifier =
            (
                if (wideLandscape) {
                    Modifier
                        .fillMaxHeight()
                        .widthIn(max = QueueLandscapeMaxWidth)
                        .fillMaxWidth()
                } else {
                    Modifier.fillMaxSize()
                }
                ).testTag(PlayerQueueTestTags.ContentPane),
        ) {
            if (compactLandscape) {
                CompactLandscapeQueueHeader(
                    queueIsNotEmpty = queue.isNotEmpty(),
                    shuffleEnabled = shuffleEnabled,
                    repeatMode = repeatMode,
                    onCyclePlaybackMode = onCyclePlaybackMode,
                    onClear = onClear,
                )
            } else {
                QueueModeControls(
                    shuffleEnabled = shuffleEnabled,
                    repeatMode = repeatMode,
                    onCyclePlaybackMode = onCyclePlaybackMode,
                )
                QueueHeader(queueIsNotEmpty = queue.isNotEmpty(), onClear = onClear)
            }
            if (queue.isEmpty()) {
                Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    Text(
                        text = stringResource(R.string.player_queue_empty),
                        color = PlayerSecondaryContent,
                        style = MaterialTheme.typography.bodyLarge,
                    )
                }
            } else {
                LazyColumn(
                    modifier = Modifier.fillMaxSize().testTag(PlayerQueueTestTags.List),
                    contentPadding =
                    PaddingValues(
                        start = 12.dp,
                        end = 8.dp,
                        bottom = if (compactLandscape) 12.dp else 24.dp,
                    ),
                    verticalArrangement = Arrangement.spacedBy(2.dp),
                ) {
                    itemsIndexed(
                        items = queue,
                        key = { _, item -> item.queueItemId },
                        contentType = { _, _ -> "player-queue-item" },
                    ) { index, item ->
                        QueueItem(
                            item = item,
                            index = index,
                            lastIndex = queue.lastIndex,
                            isCurrent = item.queueItemId == currentQueueItemId,
                            onSelect = { onSelect(item.queueItemId) },
                            onRemove = { onRemove(item.queueItemId) },
                            onMove = { direction -> onMove(item.queueItemId, direction) },
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun QueueHeader(queueIsNotEmpty: Boolean, onClear: () -> Unit) {
    Row(
        modifier =
        Modifier
            .fillMaxWidth()
            .padding(start = 24.dp, end = 14.dp, top = 12.dp, bottom = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = stringResource(R.string.player_up_next),
                color = PlayerPrimaryContent,
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.Bold,
            )
            Text(
                text = stringResource(R.string.player_queue),
                color = PlayerSecondaryContent,
                style = MaterialTheme.typography.bodySmall,
            )
        }
        QueueClearButton(queueIsNotEmpty = queueIsNotEmpty, onClear = onClear)
    }
}

@Composable
private fun CompactLandscapeQueueHeader(
    queueIsNotEmpty: Boolean,
    shuffleEnabled: Boolean,
    repeatMode: RepeatMode,
    onCyclePlaybackMode: () -> Unit,
    onClear: () -> Unit,
) {
    Row(
        modifier =
        Modifier
            .fillMaxWidth()
            .padding(start = 20.dp, end = 10.dp, top = 2.dp, bottom = 2.dp)
            .testTag(PlayerQueueTestTags.CompactHeader),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = stringResource(R.string.player_up_next),
            modifier = Modifier.weight(1f).testTag(PlayerQueueTestTags.CompactTitle),
            color = PlayerPrimaryContent,
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.Bold,
            maxLines = 1,
        )
        PlayerPlaybackModeButton(
            shuffleEnabled = shuffleEnabled,
            repeatMode = repeatMode,
            onClick = onCyclePlaybackMode,
            showLabel = false,
            modifier = Modifier.testTag(PlayerQueueTestTags.CompactPlaybackMode),
        )
        QueueClearButton(
            queueIsNotEmpty = queueIsNotEmpty,
            onClear = onClear,
            modifier = Modifier.testTag(PlayerQueueTestTags.CompactClear),
        )
    }
}

@Composable
private fun QueueClearButton(queueIsNotEmpty: Boolean, onClear: () -> Unit, modifier: Modifier = Modifier) {
    TextButton(onClick = onClear, enabled = queueIsNotEmpty, modifier = modifier) {
        Text(
            text = stringResource(R.string.player_clear_queue),
            color = if (queueIsNotEmpty) PlayerPrimaryContent else PlayerMutedContent,
        )
    }
}

@Composable
private fun QueueModeControls(shuffleEnabled: Boolean, repeatMode: RepeatMode, onCyclePlaybackMode: () -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 6.dp),
        horizontalArrangement = Arrangement.Start,
    ) {
        PlayerPlaybackModeButton(
            shuffleEnabled = shuffleEnabled,
            repeatMode = repeatMode,
            onClick = onCyclePlaybackMode,
            showLabel = true,
        )
    }
}

@Composable
private fun QueueItem(
    item: PlayerQueueItem,
    index: Int,
    lastIndex: Int,
    isCurrent: Boolean,
    onSelect: () -> Unit,
    onRemove: () -> Unit,
    onMove: (Int) -> Unit,
) {
    var menuExpanded by remember { mutableStateOf(false) }
    var dragDistance by remember(item.queueItemId) { mutableFloatStateOf(0f) }
    val moveUpLabel = stringResource(R.string.player_move_up)
    val moveDownLabel = stringResource(R.string.player_move_down)
    val currentDescription = stringResource(R.string.player_queue_current)
    val accessibilityActions =
        buildList {
            if (index > 0) {
                add(
                    CustomAccessibilityAction(moveUpLabel) {
                        onMove(-1)
                        true
                    },
                )
            }
            if (index < lastIndex) {
                add(
                    CustomAccessibilityAction(moveDownLabel) {
                        onMove(1)
                        true
                    },
                )
            }
        }
    Row(
        modifier =
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(
                if (isCurrent) {
                    PlayerPrimaryContent.copy(
                        alpha = 0.10f,
                    )
                } else {
                    androidx.compose.ui.graphics.Color.Transparent
                },
            ).clickable(onClick = onSelect)
            .padding(start = 10.dp, end = 2.dp, top = 7.dp, bottom = 7.dp)
            .semantics {
                selected = isCurrent
                if (isCurrent) stateDescription = currentDescription
            },
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(contentAlignment = Alignment.Center) {
            MediaArtwork(
                url = item.artworkUrl,
                cacheKey = item.artworkCacheKey,
                contentDescription = null,
                fallbackImageRes = R.drawable.xymusic,
                modifier = Modifier.size(44.dp).clip(RoundedCornerShape(7.dp)),
            )
            if (isCurrent) {
                Box(
                    modifier =
                    Modifier
                        .size(
                            22.dp,
                        ).clip(CircleShape)
                        .background(PlayerPrimaryContent.copy(alpha = 0.88f)),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        Icons.Default.GraphicEq,
                        contentDescription = null,
                        modifier = Modifier.size(13.dp),
                        tint = PlayerInverseContent,
                    )
                }
            }
        }
        Spacer(modifier = Modifier.width(10.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = item.title,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                fontWeight = if (isCurrent) FontWeight.SemiBold else FontWeight.Normal,
                color = PlayerPrimaryContent,
                style = MaterialTheme.typography.bodyMedium,
            )
            Text(
                text = item.artistNames.joinToString(" / "),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                color = PlayerSecondaryContent,
                style = MaterialTheme.typography.bodySmall,
            )
        }
        Icon(
            imageVector = Icons.Default.DragHandle,
            contentDescription = stringResource(R.string.player_reorder_queue_item),
            modifier =
            Modifier
                .size(42.dp)
                .semantics { customActions = accessibilityActions }
                .pointerInput(item.queueItemId, index, lastIndex) {
                    detectDragGesturesAfterLongPress(
                        onDragEnd = { dragDistance = 0f },
                        onDragCancel = { dragDistance = 0f },
                    ) { change, amount ->
                        change.consume()
                        dragDistance += amount.y
                        if (abs(dragDistance) >= 42.dp.toPx()) {
                            val direction = if (dragDistance > 0) 1 else -1
                            if ((direction < 0 && index > 0) || (direction > 0 && index < lastIndex)) {
                                onMove(direction)
                            }
                            dragDistance = 0f
                        }
                    }
                }.padding(10.dp),
            tint = PlayerSecondaryContent,
        )
        Box {
            IconButton(onClick = { menuExpanded = true }, modifier = Modifier.size(40.dp)) {
                Icon(
                    Icons.Default.MoreVert,
                    contentDescription = stringResource(R.string.common_more_actions),
                    tint = PlayerSecondaryContent,
                )
            }
            DropdownMenu(expanded = menuExpanded, onDismissRequest = { menuExpanded = false }) {
                DropdownMenuItem(
                    text = { Text(moveUpLabel) },
                    onClick = {
                        menuExpanded = false
                        onMove(-1)
                    },
                    enabled = index > 0,
                )
                DropdownMenuItem(
                    text = { Text(moveDownLabel) },
                    onClick = {
                        menuExpanded = false
                        onMove(1)
                    },
                    enabled = index < lastIndex,
                )
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.player_remove_queue_item)) },
                    onClick = {
                        menuExpanded = false
                        onRemove()
                    },
                    leadingIcon = { Icon(Icons.Outlined.DeleteOutline, contentDescription = null) },
                )
            }
        }
    }
}

private val QueueLandscapeMaxWidth = 720.dp

internal object PlayerQueueTestTags {
    const val ContentPane = "player_queue_content_pane"
    const val CompactHeader = "player_queue_compact_header"
    const val CompactTitle = "player_queue_compact_title"
    const val CompactPlaybackMode = "player_queue_compact_playback_mode"
    const val CompactClear = "player_queue_compact_clear"
    const val List = "player_queue_list"
}
