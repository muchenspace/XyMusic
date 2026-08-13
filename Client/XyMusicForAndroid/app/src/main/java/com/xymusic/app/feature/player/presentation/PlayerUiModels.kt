package com.xymusic.app.feature.player.presentation

import androidx.annotation.StringRes
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.ui.graphics.Color
import com.xymusic.app.core.model.media.LyricsTiming
import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
import com.xymusic.app.feature.player.domain.model.PlayerState
import com.xymusic.app.feature.player.domain.model.RepeatMode

@Immutable
data class PlayerLyricWordUi(val timeMs: Long, val text: String, val endTimeMs: Long? = null)

@Immutable
data class PlayerLyricLineUi(val timeMs: Long?, val text: String, val words: List<PlayerLyricWordUi> = emptyList())

@Immutable
data class PlayerUiState(
    val player: PlayerState = PlayerState(),
    val lyrics: List<PlayerLyricLineUi> = emptyList(),
    val lyricsLanguage: String? = null,
    val synchronizedLyrics: Boolean = false,
    val lyricsTiming: LyricsTiming = LyricsTiming.LINE,
    val sleepTimerRemainingMs: Long? = null,
)

/**
 * Keeps high-frequency playback samples in the frame clock instead of making the player tree recompose.
 * Structural player changes still produce a new state through the normal equality check.
 */
internal fun PlayerUiState.withoutPlaybackPosition(): PlayerUiState = copy(
    player = player.withoutPlaybackPosition(),
)

internal fun PlayerState.withoutPlaybackPosition(): PlayerState = copy(
    positionMs = 0L,
    positionAnchorElapsedRealtimeMs = null,
    bufferedPositionMs = 0L,
    positionDiscontinuitySequence = 0L,
)

internal fun PlayerState.isStructurallyEqualTo(other: PlayerState): Boolean =
    connectionState == other.connectionState &&
        playbackState == other.playbackState &&
        queue == other.queue &&
        currentQueueItemId == other.currentQueueItemId &&
        isPlaying == other.isPlaying &&
        durationMs == other.durationMs &&
        repeatMode == other.repeatMode &&
        shuffleEnabled == other.shuffleEnabled &&
        playbackSpeed == other.playbackSpeed &&
        sleepTimerRemainingMs == other.sleepTimerRemainingMs &&
        failure == other.failure

/**
 * Keeps the queue collection stable across position-only player updates.
 * The queue is replaced as a whole by the player repository and is never mutated in place.
 */
@Immutable
internal data class PlayerQueueUiState(val items: List<PlayerQueueItem>)

sealed interface PlayerUiEffect {
    data class ShowMessage(@StringRes val messageRes: Int) : PlayerUiEffect
}

enum class PlayerContentTab {
    Artwork,
    Lyrics,
    Queue,
}

internal enum class PlayerPlaybackMode {
    Shuffle,
    RepeatAll,
    RepeatOne,
}

internal val PlayerState.playbackMode: PlayerPlaybackMode
    get() =
        when {
            shuffleEnabled -> PlayerPlaybackMode.Shuffle
            repeatMode == RepeatMode.ONE -> PlayerPlaybackMode.RepeatOne
            else -> PlayerPlaybackMode.RepeatAll
        }

internal fun PlayerState.nextPlaybackMode(): PlayerPlaybackMode = when (playbackMode) {
    PlayerPlaybackMode.RepeatAll -> PlayerPlaybackMode.RepeatOne
    PlayerPlaybackMode.RepeatOne -> PlayerPlaybackMode.Shuffle
    PlayerPlaybackMode.Shuffle -> PlayerPlaybackMode.RepeatAll
}

// The player uses the current theme's foreground so light and dark modes keep
// their intended contrast over the themed backdrop.
internal val PlayerPrimaryContent: Color
    @Composable
    @ReadOnlyComposable
    get() = MaterialTheme.colorScheme.onBackground

internal val PlayerSecondaryContent: Color
    @Composable
    @ReadOnlyComposable
    get() = MaterialTheme.colorScheme.onBackground.copy(alpha = 0.72f)

internal val PlayerMutedContent: Color
    @Composable
    @ReadOnlyComposable
    get() = MaterialTheme.colorScheme.onBackground.copy(alpha = 0.38f)

internal val PlayerSubtleContent: Color
    @Composable
    @ReadOnlyComposable
    get() = MaterialTheme.colorScheme.onBackground.copy(alpha = 0.16f)

internal val PlayerInverseContent: Color
    @Composable
    @ReadOnlyComposable
    get() = MaterialTheme.colorScheme.surface
