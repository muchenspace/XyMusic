package com.xymusic.app.feature.playlist.presentation

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.PlaylistPlay
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.VerticalDivider
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import com.xymusic.app.R
import com.xymusic.app.core.ui.component.EmptyState
import com.xymusic.app.core.ui.component.ErrorState
import com.xymusic.app.core.ui.component.LoadingState
import com.xymusic.app.ui.theme.spacing

internal object PlaylistDetailTestTags {
    const val LandscapeHeader = "playlist-landscape-header"
    const val LandscapeTracks = "playlist-landscape-tracks"

    fun track(entryId: String): String = "playlist-landscape-track-$entryId"
}

internal data class PlaylistLandscapeActions(
    val onBack: () -> Unit,
    val onRefresh: () -> Unit,
    val onLoadMore: () -> Unit,
    val onPlay: (String?) -> Unit,
    val onEdit: () -> Unit,
    val onDelete: () -> Unit,
    val onRemove: (String) -> Unit,
    val onReorder: (List<String>) -> Unit,
    val onTrackMore: (String) -> Unit,
    val onMenuExpandedChange: (Boolean) -> Unit,
)

@Composable
internal fun PlaylistLandscapeScreen(
    uiState: PlaylistUiState,
    snackbarHostState: SnackbarHostState,
    reorderState: PlaylistReorderState,
    menuExpanded: Boolean,
    compactLandscape: Boolean,
    actions: PlaylistLandscapeActions,
    modifier: Modifier = Modifier,
) {
    val detail = uiState.detail
    val listState = rememberLazyListState()
    Scaffold(
        modifier =
        modifier
            .background(MaterialTheme.colorScheme.background)
            .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal)),
        topBar = {
            PlaylistTopBar(
                title = detail?.name ?: stringResource(R.string.playlist_title),
                collapsed = true,
                refreshing = uiState.isRefreshing,
                menuExpanded = menuExpanded,
                menuEnabled = detail != null && !uiState.isMutating,
                onBack = actions.onBack,
                onRefresh = actions.onRefresh,
                onMenuExpandedChange = actions.onMenuExpandedChange,
                onEdit = actions.onEdit,
                onDelete = actions.onDelete,
            )
        },
        snackbarHost = { SnackbarHost(snackbarHostState) },
        contentWindowInsets = WindowInsets(0, 0, 0, 0),
    ) { contentPadding ->
        when {
            detail == null && uiState.refreshFailed ->
                ErrorState(
                    onRetry = actions.onRefresh,
                    modifier = Modifier.fillMaxSize().padding(contentPadding),
                )

            detail == null ->
                LoadingState(modifier = Modifier.fillMaxSize().padding(contentPadding))

            else ->
                Row(modifier = Modifier.fillMaxSize().padding(contentPadding)) {
                    PlaylistLandscapeHeader(
                        detail = detail,
                        onPlayAll = { actions.onPlay(null) },
                        onEdit = actions.onEdit,
                        playAllEnabled =
                        uiState.entriesComplete && detail.entries.isNotEmpty() && !uiState.isMutating,
                        modifier =
                        Modifier
                            .weight(0.42f)
                            .fillMaxHeight()
                            .testTag(PlaylistDetailTestTags.LandscapeHeader),
                    )
                    VerticalDivider(modifier = Modifier.fillMaxHeight())
                    LazyColumn(
                        state = listState,
                        modifier =
                        Modifier
                            .weight(0.58f)
                            .fillMaxHeight()
                            .testTag(PlaylistDetailTestTags.LandscapeTracks),
                        contentPadding =
                        PaddingValues(
                            bottom =
                            if (compactLandscape) {
                                MaterialTheme.spacing.small
                            } else {
                                MaterialTheme.spacing.extraLarge
                            },
                        ),
                    ) {
                        if (uiState.isRefreshing || uiState.isMutating) {
                            item(key = "playlist-progress") {
                                LinearProgressIndicator(
                                    modifier = Modifier.fillMaxWidth(),
                                    color = MaterialTheme.colorScheme.primary,
                                )
                            }
                        }
                        item(key = "playlist-track-toolbar") {
                            PlaylistTrackToolbar(
                                trackCount = detail.trackCount,
                                refreshing = uiState.isRefreshing,
                                onPlayAll = { actions.onPlay(null) },
                                onRefresh = actions.onRefresh,
                                onEdit = actions.onEdit,
                                onMore = { actions.onMenuExpandedChange(true) },
                                playAllEnabled =
                                uiState.entriesComplete && detail.entries.isNotEmpty() && !uiState.isMutating,
                                compact = compactLandscape,
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
                            itemsIndexed(reorderState.entries, key = { _, item -> item.entryId }) { index, entry ->
                                PlaylistTrackRow(
                                    entry = entry,
                                    index = index,
                                    lastIndex = reorderState.entries.lastIndex,
                                    enabled = !uiState.isMutating,
                                    removeEnabled = !uiState.isMutating,
                                    reorderEnabled = uiState.entriesComplete && !uiState.isMutating,
                                    onPlay = { actions.onPlay(entry.entryId) },
                                    onMove = { direction -> reorderState.move(entry.entryId, direction) },
                                    onReorderFinished = {
                                        reorderState.finish()?.let(actions.onReorder)
                                    },
                                    onReorderCancelled = reorderState::cancel,
                                    onRemove = { actions.onRemove(entry.entryId) },
                                    onMore = { actions.onTrackMore(entry.track.id) },
                                    compact = compactLandscape,
                                    modifier = Modifier.testTag(PlaylistDetailTestTags.track(entry.entryId)),
                                )
                            }
                        }
                        if (uiState.hasMore) {
                            item(key = "playlist-load-more") {
                                PlaylistLoadMoreFooter(
                                    isLoading = uiState.isLoadingMore,
                                    failed = uiState.loadMoreFailed,
                                    onLoadMore = actions.onLoadMore,
                                    compact = compactLandscape,
                                )
                            }
                        }
                    }
                }
        }
    }
}
