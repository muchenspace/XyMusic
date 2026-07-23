@file:OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)

package com.xymusic.app.feature.player.presentation

import android.app.Activity
import androidx.activity.compose.BackHandler
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.tween
import androidx.compose.foundation.gestures.Orientation
import androidx.compose.foundation.gestures.draggable
import androidx.compose.foundation.gestures.rememberDraggableState
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.systemBars
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.State
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawWithCache
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import com.xymusic.app.R
import com.xymusic.app.core.ui.component.rememberArtworkAmbientColor
import com.xymusic.app.ui.theme.XyMotion
import kotlinx.coroutines.launch

@Composable
fun PlayerScreen(
    uiState: PlayerUiState,
    onBack: () -> Unit,
    onTogglePlayback: () -> Unit,
    onSeek: (Long) -> Unit,
    onPrevious: () -> Unit,
    onNext: () -> Unit,
    onCyclePlaybackMode: () -> Unit,
    onSelectQueueItem: (String) -> Unit,
    onRemoveQueueItem: (String) -> Unit,
    onMoveQueueItem: (String, Int) -> Unit,
    onClearQueue: () -> Unit,
    onPlaybackSpeedChange: (Float) -> Unit,
    onSleepTimerChange: (Int?) -> Unit,
    onToggleFavorite: () -> Unit,
    onAddToPlaylist: () -> Unit,
    playbackPosition: State<Float>? = null,
    modifier: Modifier = Modifier,
    isFavorite: Boolean = false,
) {
    val portraitPagerState =
        rememberPagerState(
            initialPage = PlayerContentTab.Artwork.ordinal,
            pageCount = { PlayerContentTab.entries.size },
        )
    val landscapePagerState =
        rememberPagerState(
            initialPage = LandscapePlayerPage.NowPlaying.ordinal,
            pageCount = { LandscapePlayerPage.entries.size },
        )
    var confirmClearQueue by rememberSaveable { mutableStateOf(false) }
    var showSpeedDialog by rememberSaveable { mutableStateOf(false) }
    var showSleepTimerDialog by rememberSaveable { mutableStateOf(false) }
    val current = uiState.player.currentItem
    val displayedPlaybackPosition = playbackPosition ?: rememberPlaybackPositionState(uiState.player)
    var draggedPosition by remember(current?.queueItemId) { mutableStateOf<Float?>(null) }
    val colorScheme = MaterialTheme.colorScheme
    val darkPlayer = colorScheme.background.luminance() < 0.5f
    val ambientColor = rememberArtworkAmbientColor(current?.artworkUrl, current?.artworkCacheKey)
    val targetBase =
        if (darkPlayer) {
            ambientColor ?: Color(0xFF443A3B)
        } else {
            colorScheme.primaryContainer
        }
    val animatedBase = animateColorAsState(
        targetValue = targetBase,
        animationSpec = tween(XyMotion.Emphasized),
        label = "playerAmbientColor",
    )
    val backgroundModifier =
        Modifier.drawWithCache {
            val currentBase = animatedBase.value
            val backgroundBrush =
                if (darkPlayer) {
                    Brush.verticalGradient(
                        0f to lerp(currentBase, Color.Black, 0.30f),
                        0.48f to lerp(currentBase, Color.Black, 0.54f),
                        1f to lerp(currentBase, Color.Black, 0.82f),
                    )
                } else {
                    Brush.verticalGradient(
                        0f to currentBase,
                        0.46f to colorScheme.surface,
                        1f to colorScheme.background,
                    )
                }
            onDrawBehind { drawRect(backgroundBrush) }
        }

    val density = LocalDensity.current
    val dismissThreshold = with(density) { 180.dp.toPx() }
    val dismissVelocityThreshold = with(density) { 1_000.dp.toPx() }
    val dismissOffset = remember { Animatable(0f) }
    var isDismissing by remember { mutableStateOf(false) }
    val dismissScope = rememberCoroutineScope()

    if (confirmClearQueue) {
        PlayerAlertDialog(
            onDismissRequest = { confirmClearQueue = false },
            title = stringResource(R.string.player_clear_queue_title),
            message = stringResource(R.string.player_clear_queue_message),
            confirmLabel = stringResource(R.string.player_clear_queue),
            onConfirm = {
                confirmClearQueue = false
                onClearQueue()
            },
        )
    }
    BoxWithConstraints(modifier = modifier.fillMaxSize()) {
        val dismissTargetOffset = with(density) { maxHeight.toPx() }.coerceAtLeast(dismissThreshold)
        val dismissPlayer: (Float) -> Unit = { releaseVelocity ->
            if (!isDismissing) {
                isDismissing = true
                dismissScope.launch {
                    dismissOffset.animateTo(
                        targetValue = dismissTargetOffset,
                        animationSpec = XyMotion.SnapSpring,
                        initialVelocity = releaseVelocity.coerceAtLeast(0f),
                    )
                }
                onBack()
            }
        }
        val dismissGestureState =
            rememberDraggableState { dragDelta ->
                if (!isDismissing) {
                    dismissScope.launch {
                        dismissOffset.snapTo(
                            (dismissOffset.value + dragDelta).coerceIn(0f, dismissTargetOffset),
                        )
                    }
                }
            }
        val dismissGestureModifier =
            Modifier.draggable(
                state = dismissGestureState,
                orientation = Orientation.Vertical,
                enabled = !isDismissing,
                startDragImmediately = dismissOffset.isRunning,
                onDragStarted = {
                    dismissScope.launch { dismissOffset.stop() }
                },
                onDragStopped = { releaseVelocity ->
                    if (!isDismissing) {
                        when (
                            resolvePlayerDismissTarget(
                                offsetPx = dismissOffset.value,
                                releaseVelocityPxPerSecond = releaseVelocity,
                                distanceThresholdPx = dismissThreshold,
                                velocityThresholdPxPerSecond = dismissVelocityThreshold,
                            )
                        ) {
                            PlayerDismissTarget.Dismiss -> dismissPlayer(releaseVelocity)
                            PlayerDismissTarget.Restore ->
                                dismissScope.launch {
                                    dismissOffset.animateTo(
                                        targetValue = 0f,
                                        animationSpec = XyMotion.SnapSpring,
                                        initialVelocity = releaseVelocity,
                                    )
                                }
                        }
                    }
                },
            )
        BackHandler(
            enabled =
                !isDismissing && !confirmClearQueue && !showSpeedDialog && !showSleepTimerDialog,
        ) {
            dismissPlayer(0f)
        }
        val isLandscape = maxWidth > maxHeight
        LandscapeStatusBarEffect(hidden = isLandscape)
        LandscapeKeepScreenOnEffect(enabled = isLandscape)
        if (!isLandscape && showSpeedDialog) {
            PlayerChoiceDialog(
                title = stringResource(R.string.player_playback_speed),
                options = PLAYBACK_SPEED_OPTIONS.map(::formatPlaybackSpeed),
                selectedIndex = PLAYBACK_SPEED_OPTIONS.indexOf(uiState.player.playbackSpeed),
                onSelect = { index ->
                    onPlaybackSpeedChange(PLAYBACK_SPEED_OPTIONS[index])
                    showSpeedDialog = false
                },
                onDismiss = { showSpeedDialog = false },
            )
        }
        if (!isLandscape && showSleepTimerDialog) {
            val options =
                listOf(
                    stringResource(R.string.player_sleep_timer_minutes, 15),
                    stringResource(R.string.player_sleep_timer_minutes, 30),
                    stringResource(R.string.player_sleep_timer_minutes, 60),
                ).toMutableList()
            if (uiState.sleepTimerRemainingMs != null) {
                options += stringResource(R.string.player_sleep_timer_off)
            }
            PlayerChoiceDialog(
                title = stringResource(R.string.player_sleep_timer),
                options = options,
                selectedIndex = -1,
                onSelect = { index ->
                    if (index in SLEEP_TIMER_MINUTE_OPTIONS.indices) {
                        onSleepTimerChange(SLEEP_TIMER_MINUTE_OPTIONS[index])
                    } else {
                        onSleepTimerChange(null)
                    }
                    showSleepTimerDialog = false
                },
                onDismiss = { showSleepTimerDialog = false },
            )
        }
        Box(
            modifier =
            Modifier
                .fillMaxSize()
                .graphicsLayer {
                    translationY = dismissOffset.value
                }.then(
                    if (!isLandscape &&
                        portraitPagerState.currentPage == PlayerContentTab.Artwork.ordinal
                    ) {
                        dismissGestureModifier
                    } else {
                        Modifier
                    },
                ).then(backgroundModifier),
        ) {
            if (isLandscape) {
                if (current == null) {
                    EmptyPlayer(
                        modifier =
                        Modifier
                            .fillMaxSize()
                            .windowInsetsPadding(WindowInsets.safeDrawing),
                    )
                } else {
                    HorizontalPager(
                        state = landscapePagerState,
                        modifier =
                        Modifier
                            .fillMaxSize()
                            .windowInsetsPadding(WindowInsets.safeDrawing)
                            .testTag(PlayerTestTags.ContentPager),
                        key = { page -> LandscapePlayerPage.entries[page] },
                        beyondViewportPageCount = 1,
                    ) { page ->
                        when (LandscapePlayerPage.entries[page]) {
                            LandscapePlayerPage.NowPlaying ->
                                LandscapeNowPlayingContent(
                                    item = current,
                                    uiState = uiState,
                                    playbackPosition = displayedPlaybackPosition,
                                    onSeek = onSeek,
                                    onTogglePlayback = onTogglePlayback,
                                    onPrevious = onPrevious,
                                    onNext = onNext,
                                    leftPaneModifier = dismissGestureModifier,
                                    modifier = Modifier.fillMaxSize(),
                                )
                            LandscapePlayerPage.Queue ->
                                QueueContent(
                                    queue = uiState.player.queue,
                                    currentQueueItemId = uiState.player.currentQueueItemId,
                                    shuffleEnabled = uiState.player.shuffleEnabled,
                                    repeatMode = uiState.player.repeatMode,
                                    onCyclePlaybackMode = onCyclePlaybackMode,
                                    onSelect = onSelectQueueItem,
                                    onRemove = onRemoveQueueItem,
                                    onMove = onMoveQueueItem,
                                    onClear = { confirmClearQueue = true },
                                    modifier = Modifier.fillMaxSize(),
                                )
                        }
                    }
                }
            } else {
                Column(
                    modifier =
                    Modifier
                        .fillMaxSize()
                        .windowInsetsPadding(WindowInsets.systemBars),
                ) {
                    PlayerTopBar(
                        onBack = onBack,
                        playbackSpeed = uiState.player.playbackSpeed,
                        sleepTimerRemainingMs = uiState.sleepTimerRemainingMs,
                        onShowSpeed = { showSpeedDialog = true },
                        onShowSleepTimer = { showSleepTimerDialog = true },
                    )
                    if (current == null) {
                        EmptyPlayer(modifier = Modifier.weight(1f))
                    } else {
                        BoxWithConstraints(modifier = Modifier.weight(1f)) {
                            val wideLayout = maxWidth >= 700.dp || maxWidth > maxHeight
                            val compactControls = wideLayout || maxHeight < 560.dp
                            Column(modifier = Modifier.fillMaxSize()) {
                                HorizontalPager(
                                    state = portraitPagerState,
                                    modifier =
                                    Modifier
                                        .weight(1f)
                                        .testTag(PlayerTestTags.ContentPager),
                                    key = { page -> PlayerContentTab.entries[page] },
                                    beyondViewportPageCount = 1,
                                ) { page ->
                                    val tab = PlayerContentTab.entries[page]
                                    when (tab) {
                                        PlayerContentTab.Artwork ->
                                            NowPlayingContent(
                                                item = current,
                                                isFavorite = isFavorite,
                                                shuffleEnabled = uiState.player.shuffleEnabled,
                                                repeatMode = uiState.player.repeatMode,
                                                onToggleFavorite = onToggleFavorite,
                                                onCyclePlaybackMode = onCyclePlaybackMode,
                                                onAddToPlaylist = onAddToPlaylist,
                                                wideLayout = wideLayout,
                                                modifier = Modifier.fillMaxSize(),
                                            )
                                        PlayerContentTab.Lyrics ->
                                            LyricsContent(
                                                uiState = uiState,
                                                onSeek = onSeek,
                                                playbackPosition = displayedPlaybackPosition,
                                                modifier = Modifier.fillMaxSize(),
                                            )
                                        PlayerContentTab.Queue ->
                                            QueueContent(
                                                queue = uiState.player.queue,
                                                currentQueueItemId = uiState.player.currentQueueItemId,
                                                shuffleEnabled = uiState.player.shuffleEnabled,
                                                repeatMode = uiState.player.repeatMode,
                                                onCyclePlaybackMode = onCyclePlaybackMode,
                                                onSelect = onSelectQueueItem,
                                                onRemove = onRemoveQueueItem,
                                                onMove = onMoveQueueItem,
                                                onClear = { confirmClearQueue = true },
                                                modifier = Modifier.fillMaxSize(),
                                            )
                                    }
                                }
                                PlaybackControls(
                                    uiState = uiState,
                                    playbackPosition = displayedPlaybackPosition,
                                    draggedPosition = draggedPosition,
                                    onPositionChange = { draggedPosition = it },
                                    onPositionChangeFinished = {
                                        draggedPosition?.let { onSeek(it.toLong()) }
                                        draggedPosition = null
                                    },
                                    onTogglePlayback = onTogglePlayback,
                                    onPrevious = onPrevious,
                                    onNext = onNext,
                                    compact = compactControls,
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

private enum class LandscapePlayerPage {
    NowPlaying,
    Queue,
}

@Composable
private fun LandscapeStatusBarEffect(hidden: Boolean) {
    val view = LocalView.current
    DisposableEffect(hidden, view) {
        val window = (view.context as? Activity)?.window
        val controller = window?.let { WindowCompat.getInsetsController(it, view) }
        val previousBehavior = controller?.systemBarsBehavior
        if (hidden) {
            controller?.systemBarsBehavior =
                WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
            controller?.hide(WindowInsetsCompat.Type.statusBars())
        } else {
            controller?.show(WindowInsetsCompat.Type.statusBars())
        }
        onDispose {
            if (hidden) {
                controller?.show(WindowInsetsCompat.Type.statusBars())
                if (previousBehavior != null) {
                    controller?.systemBarsBehavior = previousBehavior
                }
            }
        }
    }
}

@Composable
private fun LandscapeKeepScreenOnEffect(enabled: Boolean) {
    val view = LocalView.current
    DisposableEffect(enabled, view) {
        val previousKeepScreenOn = view.keepScreenOn
        if (enabled) {
            view.keepScreenOn = true
        }
        onDispose {
            if (enabled) {
                view.keepScreenOn = previousKeepScreenOn
            }
        }
    }
}
