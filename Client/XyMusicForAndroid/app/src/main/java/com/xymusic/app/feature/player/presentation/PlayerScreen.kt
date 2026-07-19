@file:OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)

package com.xymusic.app.feature.player.presentation

import android.app.Activity
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.snap
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectVerticalDragGestures
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
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.input.pointer.pointerInput
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
    val animatedBase by animateColorAsState(
        targetValue = targetBase,
        animationSpec = tween(XyMotion.Emphasized),
        label = "playerAmbientColor",
    )
    val backgroundBrush =
        remember(
            animatedBase,
            darkPlayer,
            colorScheme.surface,
            colorScheme.background,
        ) {
            if (darkPlayer) {
                Brush.verticalGradient(
                    0f to lerp(animatedBase, Color.Black, 0.30f),
                    0.48f to lerp(animatedBase, Color.Black, 0.54f),
                    1f to lerp(animatedBase, Color.Black, 0.82f),
                )
            } else {
                Brush.verticalGradient(
                    0f to animatedBase,
                    0.46f to colorScheme.surface,
                    1f to colorScheme.background,
                )
            }
        }

    val density = LocalDensity.current
    val dismissThreshold = with(density) { 180.dp.toPx() }
    var dragOffset by remember { mutableFloatStateOf(0f) }
    var isDraggingPlayer by remember { mutableStateOf(false) }
    val animatedDragOffset by animateFloatAsState(
        targetValue = dragOffset,
        animationSpec =
        if (isDraggingPlayer) {
            snap()
        } else {
            spring(
                dampingRatio = Spring.DampingRatioMediumBouncy,
                stiffness = Spring.StiffnessMedium,
            )
        },
        label = "playerDragOffset",
    )

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
        val dismissGestureModifier =
            Modifier.pointerInput(dismissThreshold, onBack) {
                detectVerticalDragGestures(
                    onDragStart = { isDraggingPlayer = true },
                    onDragEnd = {
                        isDraggingPlayer = false
                        if (dragOffset > dismissThreshold) onBack() else dragOffset = 0f
                    },
                    onDragCancel = {
                        isDraggingPlayer = false
                        dragOffset = 0f
                    },
                ) { _, dragAmount ->
                    dragOffset = (dragOffset + dragAmount).coerceAtLeast(0f)
                }
            }

        Box(
            modifier =
            Modifier
                .fillMaxSize()
                .graphicsLayer {
                    translationY = animatedDragOffset
                    alpha = 1f - (animatedDragOffset / (dismissThreshold * 2f)).coerceIn(0f, 0.35f)
                }.then(
                    if (!isLandscape &&
                        portraitPagerState.currentPage == PlayerContentTab.Artwork.ordinal
                    ) {
                        dismissGestureModifier
                    } else {
                        Modifier
                    },
                ).background(backgroundBrush),
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
