package com.xymusic.app.feature.playlist.presentation

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.PlaylistPlay
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.xymusic.app.R
import com.xymusic.app.core.ui.component.EmptyState
import com.xymusic.app.core.ui.component.ErrorState
import com.xymusic.app.core.ui.component.LoadingState
import com.xymusic.app.core.ui.layout.isCompactLandscape
import com.xymusic.app.core.ui.layout.isWideLandscape
import com.xymusic.app.feature.playlist.domain.model.PlaylistVisibility
import com.xymusic.app.ui.theme.spacing

@Composable
fun PlaylistRoute(
    onBack: () -> Unit,
    onDeleted: () -> Unit,
    onTrackMore: (String) -> Unit,
    modifier: Modifier = Modifier,
    viewModel: PlaylistViewModel = hiltViewModel(),
) {
    val uiState by viewModel.uiState.collectAsStateWithLifecycle()
    val snackbarHostState = remember { SnackbarHostState() }
    val resources = LocalResources.current
    LaunchedEffect(viewModel, snackbarHostState, resources) {
        viewModel.effects.collect { effect ->
            when (effect) {
                PlaylistUiEffect.Deleted -> onDeleted()
                is PlaylistUiEffect.ShowMessage -> {
                    snackbarHostState.showSnackbar(resources.getString(effect.messageRes))
                }
            }
        }
    }
    PlaylistScreen(
        uiState = uiState,
        snackbarHostState = snackbarHostState,
        onBack = onBack,
        onRefresh = viewModel::refresh,
        onLoadMore = viewModel::loadMore,
        onPlay = viewModel::playFrom,
        onUpdate = viewModel::update,
        onDelete = viewModel::delete,
        onRemove = viewModel::remove,
        onReorder = viewModel::reorder,
        onTrackMore = onTrackMore,
        modifier = modifier,
    )
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PlaylistScreen(
    uiState: PlaylistUiState,
    snackbarHostState: SnackbarHostState,
    onBack: () -> Unit,
    onRefresh: () -> Unit,
    onLoadMore: () -> Unit,
    onPlay: (String?) -> Unit,
    onUpdate: (String, String?, PlaylistVisibility) -> Unit,
    onDelete: () -> Unit,
    onRemove: (String) -> Unit,
    onReorder: (List<String>) -> Unit,
    onTrackMore: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    var showEditDialog by remember { mutableStateOf(false) }
    var showDeleteDialog by remember { mutableStateOf(false) }
    var menuExpanded by remember { mutableStateOf(false) }
    val detail = uiState.detail
    val reorderState = remember(detail?.id) { PlaylistReorderState(detail?.entries.orEmpty()) }
    LaunchedEffect(detail?.entries) {
        reorderState.sync(detail?.entries.orEmpty())
    }
    val listState = rememberLazyListState()
    val collapsedTopBar by remember(listState) {
        derivedStateOf {
            listState.firstVisibleItemIndex > 0 || listState.firstVisibleItemScrollOffset > 180
        }
    }

    if (showEditDialog && detail != null) {
        PlaylistEditorDialog(
            title = stringResource(R.string.playlist_edit),
            submitLabel = stringResource(R.string.common_confirm),
            initialName = detail.name,
            initialDescription = detail.description.orEmpty(),
            initialVisibility = detail.visibility,
            onDismiss = { showEditDialog = false },
            onSubmit = { name, description, visibility ->
                onUpdate(name, description, visibility)
                showEditDialog = false
            },
        )
    }
    if (showDeleteDialog) {
        AlertDialog(
            onDismissRequest = { showDeleteDialog = false },
            title = { Text(stringResource(R.string.playlist_delete_title)) },
            text = { Text(stringResource(R.string.playlist_delete_message)) },
            confirmButton = {
                TextButton(
                    onClick = {
                        showDeleteDialog = false
                        onDelete()
                    },
                ) { Text(stringResource(R.string.playlist_delete)) }
            },
            dismissButton = {
                TextButton(onClick = { showDeleteDialog = false }) {
                    Text(stringResource(R.string.common_cancel))
                }
            },
        )
    }

    BoxWithConstraints(modifier = modifier) {
        val wideLandscape = isWideLandscape(maxWidth, maxHeight)
        if (wideLandscape) {
            PlaylistLandscapeScreen(
                uiState = uiState,
                snackbarHostState = snackbarHostState,
                reorderState = reorderState,
                menuExpanded = menuExpanded,
                compactLandscape = isCompactLandscape(maxWidth, maxHeight),
                actions =
                PlaylistLandscapeActions(
                    onBack = onBack,
                    onRefresh = onRefresh,
                    onLoadMore = onLoadMore,
                    onPlay = onPlay,
                    onEdit = { showEditDialog = true },
                    onDelete = { showDeleteDialog = true },
                    onRemove = onRemove,
                    onReorder = onReorder,
                    onTrackMore = onTrackMore,
                    onMenuExpandedChange = { menuExpanded = it },
                ),
                modifier = Modifier.fillMaxSize(),
            )
        } else {
            Scaffold(
                modifier = Modifier.background(MaterialTheme.colorScheme.background),
                snackbarHost = { SnackbarHost(snackbarHostState) },
                contentWindowInsets = WindowInsets(0, 0, 0, 0),
            ) { contentPadding ->
                Box(
                    modifier =
                    Modifier
                        .fillMaxSize()
                        .padding(contentPadding),
                ) {
                    when {
                        detail == null && uiState.refreshFailed ->
                            ErrorState(
                                onRetry = onRefresh,
                                modifier =
                                Modifier
                                    .fillMaxSize()
                                    .padding(contentPadding),
                            )

                        detail == null ->
                            LoadingState(
                                modifier =
                                Modifier
                                    .fillMaxSize()
                                    .padding(contentPadding),
                            )

                        else -> {
                            LazyColumn(
                                state = listState,
                                modifier = Modifier.fillMaxSize(),
                                contentPadding = PaddingValues(bottom = MaterialTheme.spacing.extraLarge),
                            ) {
                                if (uiState.isRefreshing || uiState.isMutating) {
                                    item(key = "playlist-progress") {
                                        LinearProgressIndicator(
                                            modifier = Modifier.fillMaxWidth(),
                                            color = MaterialTheme.colorScheme.primary,
                                        )
                                    }
                                }
                                item(key = "playlist-header") {
                                    PlaylistHeader(
                                        detail = detail,
                                        onPlayAll = { onPlay(null) },
                                        onEdit = { showEditDialog = true },
                                        playAllEnabled =
                                        uiState.entriesComplete && detail.entries.isNotEmpty() && !uiState.isMutating,
                                    )
                                }
                                item(key = "playlist-track-toolbar") {
                                    PlaylistTrackToolbar(
                                        trackCount = detail.trackCount,
                                        refreshing = uiState.isRefreshing,
                                        onPlayAll = { onPlay(null) },
                                        onRefresh = onRefresh,
                                        onEdit = { showEditDialog = true },
                                        onMore = { menuExpanded = true },
                                        playAllEnabled =
                                        uiState.entriesComplete && detail.entries.isNotEmpty() && !uiState.isMutating,
                                    )
                                }
                                if (detail.entries.isEmpty()) {
                                    item(key = "playlist-empty") {
                                        EmptyState(
                                            title = stringResource(R.string.playlist_tracks_empty_title),
                                            message = stringResource(R.string.playlist_tracks_empty_message),
                                            icon = Icons.AutoMirrored.Outlined.PlaylistPlay,
                                        )
                                    }
                                } else {
                                    itemsIndexed(
                                        reorderState.entries,
                                        key = { _, item -> item.entryId },
                                    ) { index, entry ->
                                        PlaylistTrackRow(
                                            entry = entry,
                                            index = index,
                                            lastIndex = reorderState.entries.lastIndex,
                                            enabled = !uiState.isMutating,
                                            removeEnabled = !uiState.isMutating,
                                            reorderEnabled = uiState.entriesComplete && !uiState.isMutating,
                                            onPlay = { onPlay(entry.entryId) },
                                            onMove = { direction -> reorderState.move(entry.entryId, direction) },
                                            onReorderFinished = {
                                                reorderState.finish()?.let(onReorder)
                                            },
                                            onReorderCancelled = reorderState::cancel,
                                            onRemove = { onRemove(entry.entryId) },
                                            onMore = { onTrackMore(entry.track.id) },
                                        )
                                    }
                                }
                                if (uiState.hasMore) {
                                    item(key = "playlist-load-more") {
                                        PlaylistLoadMoreFooter(
                                            isLoading = uiState.isLoadingMore,
                                            failed = uiState.loadMoreFailed,
                                            onLoadMore = onLoadMore,
                                        )
                                    }
                                }
                            }
                        }
                    }
                    PlaylistTopBar(
                        title = detail?.name ?: stringResource(R.string.playlist_title),
                        collapsed = collapsedTopBar || detail == null,
                        refreshing = uiState.isRefreshing,
                        menuExpanded = menuExpanded,
                        menuEnabled = detail != null && !uiState.isMutating,
                        onBack = onBack,
                        onRefresh = onRefresh,
                        onMenuExpandedChange = { menuExpanded = it },
                        onEdit = { showEditDialog = true },
                        onDelete = { showDeleteDialog = true },
                    )
                }
            }
        }
    }
}
