package com.xymusic.app.feature.player.presentation

import androidx.annotation.StringRes
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.luminance
import com.xymusic.app.feature.player.domain.model.PlayerState
import com.xymusic.app.feature.player.domain.model.RepeatMode

@Immutable
data class PlayerLyricLineUi(val timeMs: Long?, val text: String, val highlightEndOffsets: List<Int> = emptyList())

@Immutable
data class PlayerLyricProgressUi(
    val lineIndex: Int,
    val highlightedTextEndIndex: Int,
    val lineEndTimeMs: Long,
    val lineProgress: Float,
)

@Immutable
data class PlayerUiState(
    val player: PlayerState = PlayerState(),
    val lyrics: List<PlayerLyricLineUi> = emptyList(),
    val lyricsLanguage: String? = null,
    val synchronizedLyrics: Boolean = false,
    val wordByWordLyricsEnabled: Boolean = true,
    val sleepTimerRemainingMs: Long? = null,
)

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

internal val PlayerState.playbackMode: PlayerPlaybackMode?
    get() =
        when {
            shuffleEnabled -> PlayerPlaybackMode.Shuffle
            repeatMode == RepeatMode.ALL -> PlayerPlaybackMode.RepeatAll
            repeatMode == RepeatMode.ONE -> PlayerPlaybackMode.RepeatOne
            else -> null
        }

internal fun PlayerState.nextPlaybackMode(): PlayerPlaybackMode = when (playbackMode) {
    null -> PlayerPlaybackMode.Shuffle
    PlayerPlaybackMode.Shuffle -> PlayerPlaybackMode.RepeatAll
    PlayerPlaybackMode.RepeatAll -> PlayerPlaybackMode.RepeatOne
    PlayerPlaybackMode.RepeatOne -> PlayerPlaybackMode.Shuffle
}

internal val PlayerPrimaryContent: Color
    @Composable
    @ReadOnlyComposable
    get() = if (isDarkPlayerTheme()) Color.White else Color(0xFF1C1C1E)

internal val PlayerSecondaryContent: Color
    @Composable
    @ReadOnlyComposable
    get() =
        if (isDarkPlayerTheme()) {
            Color.White.copy(alpha = 0.72f)
        } else {
            Color(0xFF636366)
        }

internal val PlayerMutedContent: Color
    @Composable
    @ReadOnlyComposable
    get() =
        if (isDarkPlayerTheme()) {
            Color.White.copy(alpha = 0.42f)
        } else {
            Color(0xFF8E8E93)
        }

internal val PlayerSubtleContent: Color
    @Composable
    @ReadOnlyComposable
    get() =
        if (isDarkPlayerTheme()) {
            Color.White.copy(alpha = 0.18f)
        } else {
            Color.Black.copy(alpha = 0.12f)
        }

internal val PlayerInverseContent: Color
    @Composable
    @ReadOnlyComposable
    get() = if (isDarkPlayerTheme()) Color.Black else Color.White

@Composable
@ReadOnlyComposable
private fun isDarkPlayerTheme(): Boolean = MaterialTheme.colorScheme.background.luminance() < 0.5f
