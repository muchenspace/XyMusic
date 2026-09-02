@file:OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)

package com.xymusic.app.feature.player.presentation

import androidx.activity.compose.BackHandler
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
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
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawWithCache
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import com.xymusic.app.core.ui.component.ArtworkAmbientPalette
import com.xymusic.app.core.ui.component.rememberArtworkAmbientPalette
import com.xymusic.app.ui.theme.XyMotion
import com.xymusic.app.ui.theme.xyColors

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
    playbackPosition: State<Float>? = null,
    isFavorite: Boolean = false,
    dismissGestureModifier: Modifier = Modifier,
    immersiveLandscape: Boolean = true,
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
    val displayedPlaybackPosition =
        playbackPosition ?: rememberPlaybackPositionSnapshotState(uiState.player)
    val queueUiState = remember(uiState.player.queue) { PlayerQueueUiState(uiState.player.queue) }
    var draggedPosition by remember(current?.queueItemId) { mutableStateOf<Float?>(null) }

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
        BackHandler(
            enabled = !confirmClearQueue && !showSpeedDialog && !showSleepTimerDialog,
        ) {
            onBack()
        }
        val isLandscape = maxWidth > maxHeight
        // Landscape immersion (status bar hidden) is applied app-wide by
        // AppLandscapeSystemBarsEffect at the root. Keeping it here too would
        // fight the root effect on dispose (it re-shows the bar while the root
        // hides it again), so the player relies on the app-wide behavior.
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
                .then(
                    if (!isLandscape &&
                        portraitPagerState.currentPage == PlayerContentTab.Artwork.ordinal
                    ) {
                        dismissGestureModifier
                    } else {
                        Modifier
                    },
                ),
        ) {
            PlayerAmbientBackdrop(
                artworkUrl = current?.artworkUrl,
                artworkCacheKey = current?.artworkCacheKey,
                modifier = Modifier.fillMaxSize(),
            )
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
                                    queue = queueUiState,
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
                        item = current,
                        showTrackInfo = portraitPagerState.currentPage != PlayerContentTab.Artwork.ordinal,
                        isFavorite = isFavorite,
                        onDismiss = onBack,
                        onToggleFavorite = onToggleFavorite,
                        onAddToPlaylist = onAddToPlaylist,
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
                                ) { page ->
                                    val tab = PlayerContentTab.entries[page]
                                    when (tab) {
                                        PlayerContentTab.Artwork ->
                                            NowPlayingContent(
                                                item = current,
                                                shuffleEnabled = uiState.player.shuffleEnabled,
                                                repeatMode = uiState.player.repeatMode,
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
                                                centerActiveLine = true,
                                                modifier = Modifier.fillMaxSize(),
                                            )
                                        PlayerContentTab.Queue ->
                                            QueueContent(
                                                queue = queueUiState,
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

@Composable
private fun PlayerAmbientBackdrop(artworkUrl: String?, artworkCacheKey: String?, modifier: Modifier = Modifier) {
    val themeColors = MaterialTheme.xyColors
    val targetPalette = rememberArtworkAmbientPalette(artworkUrl, artworkCacheKey)
        ?: PlayerFallbackAmbientPalette
    val animatedPrimary = animateColorAsState(
        targetValue = targetPalette.primary,
        animationSpec = PlayerAmbientColorAnimationSpec,
        label = "playerAmbientPrimary",
    )
    val animatedSecondary = animateColorAsState(
        targetValue = targetPalette.secondary,
        animationSpec = PlayerAmbientColorAnimationSpec,
        label = "playerAmbientSecondary",
    )
    val animatedHighlight = animateColorAsState(
        targetValue = targetPalette.highlight,
        animationSpec = PlayerAmbientColorAnimationSpec,
        label = "playerAmbientHighlight",
    )
    Box(
        modifier =
        modifier
            .background(themeColors.nowPlayingBg)
            .drawWithCache {
                // Read the animated colors from draw so palette interpolation invalidates only
                // this backdrop instead of recomposing the full player tree every frame.
                val backdropColors =
                    resolvePlayerBackdropColors(
                        themeColors = themeColors,
                        ambientPalette =
                        ArtworkAmbientPalette(
                            primary = animatedPrimary.value,
                            secondary = animatedSecondary.value,
                            highlight = animatedHighlight.value,
                        ),
                    )
                val background =
                    Brush.linearGradient(
                        colorStops =
                        arrayOf(
                            0f to backdropColors.start.copy(alpha = 0.88f),
                            0.38f to backdropColors.center.copy(alpha = 0.78f),
                            0.70f to backdropColors.highlight.copy(alpha = 0.74f),
                            1f to backdropColors.end.copy(alpha = 0.90f),
                        ),
                        start = Offset(0f, 0f),
                        end = Offset(size.width, size.height),
                    )
                onDrawBehind { drawRect(background) }
            },
    )
}

private val PlayerAmbientColorAnimationSpec = tween<Color>(
    durationMillis = XyMotion.Emphasized,
    easing = XyMotion.EaseOutQuart,
)

private val PlayerFallbackAmbientPalette = ArtworkAmbientPalette(
    primary = Color(0xFF684032),
    secondary = Color(0xFF344B50),
    highlight = Color(0xFF563950),
)
