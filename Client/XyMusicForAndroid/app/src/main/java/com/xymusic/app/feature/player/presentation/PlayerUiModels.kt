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

internal fun PlayerLyricWordUi.effectiveEndTimeMs(nextWordStartMs: Long? = null): Long {
    endTimeMs?.let { return it }
    nextWordStartMs?.let { nextStart -> return nextStart.coerceAtLeast(timeMs) }
    val characterCount = text.lyricCharacterFragmentCount().coerceAtLeast(1)
    val revealDurationMs =
        (characterCount * UNTERMINATED_WORD_MILLIS_PER_CHARACTER)
            .coerceIn(UNTERMINATED_WORD_MIN_MILLIS, UNTERMINATED_WORD_MAX_MILLIS)
    return timeMs.coerceAtMost(Long.MAX_VALUE - revealDurationMs) + revealDurationMs
}

internal fun String.lyricCharacterFragmentCount(): Int {
    var count = 0
    var fragmentStart = 0
    while (fragmentStart < length) {
        val fragmentEnd = nextLyricCharacterFragmentEnd(fragmentStart, length)
        if (fragmentEnd <= fragmentStart) break
        count += 1
        fragmentStart = fragmentEnd
    }
    return count
}

internal fun String.nextLyricCharacterFragmentEnd(startOffset: Int, rangeEnd: Int): Int {
    val firstCodePoint = codePointAt(startOffset)
    var fragmentEnd = (startOffset + Character.charCount(firstCodePoint)).coerceAtMost(rangeEnd)
    if (firstCodePoint.isRegionalIndicator() && fragmentEnd < rangeEnd) {
        val nextCodePoint = codePointAt(fragmentEnd)
        if (nextCodePoint.isRegionalIndicator()) {
            fragmentEnd = (fragmentEnd + Character.charCount(nextCodePoint)).coerceAtMost(rangeEnd)
        }
    }
    while (fragmentEnd < rangeEnd) {
        val codePoint = codePointAt(fragmentEnd)
        fragmentEnd =
            when {
                isCharacterContinuation(codePoint) ->
                    (fragmentEnd + Character.charCount(codePoint)).coerceAtMost(rangeEnd)
                codePoint == ZERO_WIDTH_JOINER -> joinedCharacterEnd(fragmentEnd, rangeEnd)
                else -> break
            }
    }
    return fragmentEnd
}

private fun String.joinedCharacterEnd(joinerOffset: Int, rangeEnd: Int): Int {
    val afterJoiner = joinerOffset + Character.charCount(codePointAt(joinerOffset))
    if (afterJoiner >= rangeEnd) return afterJoiner.coerceAtMost(rangeEnd)
    return (afterJoiner + Character.charCount(codePointAt(afterJoiner))).coerceAtMost(rangeEnd)
}

private fun isCharacterContinuation(codePoint: Int): Boolean {
    val type = Character.getType(codePoint)
    return type == Character.NON_SPACING_MARK.toInt() ||
        type == Character.COMBINING_SPACING_MARK.toInt() ||
        type == Character.ENCLOSING_MARK.toInt() ||
        codePoint in VARIATION_SELECTOR_START..VARIATION_SELECTOR_END ||
        codePoint in SUPPLEMENTARY_VARIATION_SELECTOR_START..SUPPLEMENTARY_VARIATION_SELECTOR_END ||
        codePoint in EMOJI_MODIFIER_START..EMOJI_MODIFIER_END ||
        codePoint in EMOJI_TAG_START..EMOJI_TAG_END
}

private fun Int.isRegionalIndicator(): Boolean = this in REGIONAL_INDICATOR_START..REGIONAL_INDICATOR_END

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

private const val UNTERMINATED_WORD_MILLIS_PER_CHARACTER = 90L
private const val UNTERMINATED_WORD_MIN_MILLIS = 240L
private const val UNTERMINATED_WORD_MAX_MILLIS = 900L
private const val ZERO_WIDTH_JOINER = 0x200D
private const val VARIATION_SELECTOR_START = 0xFE00
private const val VARIATION_SELECTOR_END = 0xFE0F
private const val SUPPLEMENTARY_VARIATION_SELECTOR_START = 0xE0100
private const val SUPPLEMENTARY_VARIATION_SELECTOR_END = 0xE01EF
private const val EMOJI_MODIFIER_START = 0x1F3FB
private const val EMOJI_MODIFIER_END = 0x1F3FF
private const val EMOJI_TAG_START = 0xE0020
private const val EMOJI_TAG_END = 0xE007F
private const val REGIONAL_INDICATOR_START = 0x1F1E6
private const val REGIONAL_INDICATOR_END = 0x1F1FF
