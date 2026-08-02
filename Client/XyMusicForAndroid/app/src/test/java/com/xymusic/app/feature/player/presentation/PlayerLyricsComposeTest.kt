package com.xymusic.app.feature.player.presentation

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsNotSelected
import androidx.compose.ui.test.assertIsSelected
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.player.domain.model.PlayerState
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMusicTheme
import kotlin.math.abs
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(
    sdk = [34],
    application = ComposeTestApplication::class,
    qualifiers = "w740dp-h320dp-land",
)
class PlayerLyricsComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun activeLineTransitionMovesContinuouslyBeforeSettlingAtCenter() {
        val uiState =
            mutableStateOf(
                PlayerUiState(
                    player = PlayerState(positionMs = 0),
                    lyrics =
                    listOf(
                        PlayerLyricLineUi(0, "First animated lyric"),
                        PlayerLyricLineUi(1_000, "Second animated lyric"),
                        PlayerLyricLineUi(2_000, "Third animated lyric"),
                    ),
                    synchronizedLyrics = true,
                ),
            )
        composeRule.setContent {
            XyMusicTheme(dynamicColor = false) {
                Box(
                    modifier =
                    Modifier
                        .size(width = 480.dp, height = 300.dp)
                        .testTag(LYRICS_PANE_TAG),
                ) {
                    LyricsContent(
                        uiState = uiState.value,
                        onSeek = {},
                        compact = true,
                        centerActiveLine = true,
                    )
                }
            }
        }
        composeRule.waitForIdle()

        val paneCenter =
            composeRule
                .onNodeWithTag(LYRICS_PANE_TAG)
                .fetchSemanticsNode()
                .boundsInRoot
                .center.y
        val initialSecondCenter = secondLineCenter()
        val initialDistance = initialSecondCenter - paneCenter
        assertThat(initialDistance).isGreaterThan(2f)

        composeRule.mainClock.autoAdvance = false
        composeRule.runOnIdle {
            uiState.value =
                uiState.value.copy(
                    player = uiState.value.player.copy(positionMs = 1_000),
                )
        }
        composeRule.mainClock.advanceTimeByFrame()
        composeRule.waitForIdle()

        val transitionStartDistance = abs(secondLineCenter() - paneCenter)
        assertThat(transitionStartDistance).isGreaterThan(initialDistance * 0.7f)

        composeRule.mainClock.advanceTimeBy(100)
        composeRule.waitForIdle()
        val midwayDistance = abs(secondLineCenter() - paneCenter)
        assertThat(midwayDistance).isLessThan(transitionStartDistance)
        assertThat(midwayDistance).isGreaterThan(2f)

        composeRule.mainClock.advanceTimeBy(180)
        composeRule.mainClock.autoAdvance = true
        composeRule.waitForIdle()
        assertThat(abs(secondLineCenter() - paneCenter)).isLessThan(2f)
    }

    @Test
    fun portraitSeekUsesTheSettledTargetAsTheFirstFollowingTransitionBaseline() {
        val clickedText = "Portrait clicked lyric with a second visual line to exercise layout"
        val followingText = "Portrait following lyric"
        val uiState =
            mutableStateOf(
                PlayerUiState(
                    player = PlayerState(positionMs = 0),
                    lyrics =
                    listOf(
                        PlayerLyricLineUi(0, "Portrait first lyric"),
                        PlayerLyricLineUi(1_000, "Portrait second lyric"),
                        PlayerLyricLineUi(2_000, "Portrait third lyric"),
                        PlayerLyricLineUi(3_000, "Portrait fourth lyric"),
                        PlayerLyricLineUi(4_000, clickedText),
                        PlayerLyricLineUi(5_000, followingText),
                        PlayerLyricLineUi(6_000, "Portrait sixth lyric"),
                        PlayerLyricLineUi(7_000, "Portrait seventh lyric"),
                        PlayerLyricLineUi(8_000, "Portrait eighth lyric"),
                        PlayerLyricLineUi(9_000, "Portrait ninth lyric"),
                        PlayerLyricLineUi(10_000, "Portrait tenth lyric"),
                        PlayerLyricLineUi(11_000, "Portrait eleventh lyric"),
                        PlayerLyricLineUi(12_000, "Portrait last lyric"),
                    ),
                    synchronizedLyrics = true,
                ),
            )
        composeRule.setContent {
            XyMusicTheme(dynamicColor = false) {
                Box(
                    modifier =
                    Modifier
                        .size(width = 360.dp, height = 300.dp)
                        .testTag(LYRICS_PANE_TAG),
                ) {
                    LyricsContent(
                        uiState = uiState.value,
                        onSeek = { position ->
                            uiState.value = uiState.value.copy(
                                player = uiState.value.player.copy(positionMs = position),
                            )
                        },
                        compact = true,
                        centerActiveLine = false,
                    )
                }
            }
        }
        composeRule.waitForIdle()

        composeRule.onNodeWithText(clickedText).performClick()
        composeRule.waitForIdle()
        composeRule.onNodeWithText(clickedText).assertIsSelected()

        val followingBeforeTransition = lineCenter(followingText)
        composeRule.mainClock.autoAdvance = false
        composeRule.runOnIdle {
            uiState.value = uiState.value.copy(
                player = uiState.value.player.copy(positionMs = 5_000),
            )
        }
        composeRule.mainClock.advanceTimeByFrame()
        composeRule.waitForIdle()
        val transitionStart = lineCenter(followingText)
        assertThat(abs(transitionStart - followingBeforeTransition)).isLessThan(2f)

        val transitionFrameCenters = buildList {
            repeat(6) {
                composeRule.mainClock.advanceTimeByFrame()
                composeRule.waitForIdle()
                add(lineCenter(followingText))
            }
        }
        val transitionMiddle = transitionFrameCenters.last()
        val remainingFrameCenters = buildList {
            repeat(7) {
                composeRule.mainClock.advanceTimeByFrame()
                composeRule.waitForIdle()
                add(lineCenter(followingText))
            }
        }
        val largestFrameDelta = transitionFrameCenters
            .plus(remainingFrameCenters)
            .zipWithNext()
            .maxOfOrNull { (previous, current) -> abs(current - previous) }
            ?: 0f
        assertThat(largestFrameDelta).isLessThan(20f)

        composeRule.mainClock.autoAdvance = true
        composeRule.waitForIdle()
        val transitionEnd = lineCenter(followingText)
        assertThat(transitionMiddle).isLessThan(transitionStart - 2f)
        assertThat(transitionEnd).isLessThan(transitionMiddle - 2f)
    }

    @Test
    fun lyricSeekSnapsBeforeFirstFollowingLineUsesTheNormalTransition() {
        val uiState =
            mutableStateOf(
                PlayerUiState(
                    player = PlayerState(positionMs = 0),
                    lyrics =
                    listOf(
                        PlayerLyricLineUi(0, "Seek first lyric"),
                        PlayerLyricLineUi(1_000, "Seek second lyric"),
                        PlayerLyricLineUi(2_000, "Seek target lyric"),
                        PlayerLyricLineUi(3_000, "Seek following lyric"),
                        PlayerLyricLineUi(4_000, "Seek last lyric"),
                    ),
                    synchronizedLyrics = true,
                ),
            )
        composeRule.setContent {
            XyMusicTheme(dynamicColor = false) {
                Box(
                    modifier =
                    Modifier
                        .size(width = 480.dp, height = 300.dp)
                        .testTag(LYRICS_PANE_TAG),
                ) {
                    LyricsContent(
                        uiState = uiState.value,
                        onSeek = { position ->
                            uiState.value = uiState.value.copy(
                                player = uiState.value.player.copy(positionMs = position),
                            )
                        },
                        compact = true,
                        centerActiveLine = true,
                    )
                }
            }
        }
        composeRule.waitForIdle()

        composeRule.onNodeWithText("Seek target lyric").performClick()
        composeRule.waitForIdle()

        val paneCenter =
            composeRule
                .onNodeWithTag(LYRICS_PANE_TAG)
                .fetchSemanticsNode()
                .boundsInRoot
                .center.y
        assertThat(abs(targetLineCenter() - paneCenter)).isLessThan(2f)
        composeRule.onNodeWithText("Seek target lyric").assertIsSelected()

        composeRule.mainClock.autoAdvance = false
        val initialTargetCenter = targetLineCenter()
        val initialFollowingCenter = followingLineCenter()
        val initialFollowingDistance = abs(followingLineCenter() - paneCenter)
        composeRule.runOnIdle {
            uiState.value = uiState.value.copy(
                player = uiState.value.player.copy(positionMs = 3_000),
            )
        }
        composeRule.mainClock.advanceTimeByFrame()
        composeRule.waitForIdle()

        assertThat(abs(targetLineCenter() - initialTargetCenter)).isLessThan(2f)
        val transitionStartDistance = abs(followingLineCenter() - paneCenter)
        assertThat(transitionStartDistance).isGreaterThan(initialFollowingDistance * 0.7f)

        val transitionFrames = mutableListOf(initialTargetCenter to initialFollowingCenter)
        transitionFrames += targetLineCenter() to followingLineCenter()

        composeRule.mainClock.advanceTimeBy(100)
        composeRule.waitForIdle()
        val targetMiddle = targetLineCenter()
        val midwayDistance = abs(followingLineCenter() - paneCenter)
        val followingMiddle = followingLineCenter()
        transitionFrames += targetMiddle to followingMiddle
        val targetMiddleHeight = lineBounds("Seek target lyric").height
        val followingMiddleHeight = lineBounds("Seek following lyric").height
        assertThat(
            abs(
                (initialTargetCenter - targetMiddle) -
                    (initialFollowingCenter - followingMiddle),
            ),
        ).isLessThan(2f)
        assertThat(abs(targetMiddleHeight - followingMiddleHeight)).isLessThan(2f)
        assertThat(midwayDistance).isLessThan(transitionStartDistance)
        assertThat(midwayDistance).isGreaterThan(2f)

        composeRule.mainClock.advanceTimeBy(100)
        composeRule.waitForIdle()
        val transitionEnd = followingLineCenter()
        transitionFrames += targetLineCenter() to transitionEnd
        composeRule.mainClock.advanceTimeByFrame()
        composeRule.waitForIdle()
        val afterScrollCommit = followingLineCenter()
        transitionFrames += targetLineCenter() to afterScrollCommit
        assertThat(abs(afterScrollCommit - transitionEnd)).isLessThan(2f)

        composeRule.mainClock.autoAdvance = true
        composeRule.waitForIdle()
        transitionFrames += targetLineCenter() to followingLineCenter()
        val largestRelativeMovementDelta =
            transitionFrames
                .zipWithNext()
                .maxOfOrNull { (previous, current) ->
                    abs(
                        (current.first - previous.first) -
                            (current.second - previous.second),
                    )
                } ?: 0f
        assertThat(largestRelativeMovementDelta).isLessThan(2f)
        assertThat(abs(followingLineCenter() - paneCenter)).isLessThan(2f)
    }

    @Test
    fun lyricSeekDoesNotSettleOnAnIntermediateLineBeforeTheTarget() {
        val uiState =
            mutableStateOf(
                PlayerUiState(
                    player = PlayerState(positionMs = 0),
                    lyrics =
                    listOf(
                        PlayerLyricLineUi(0, "Intermediate first lyric"),
                        PlayerLyricLineUi(1_000, "Intermediate second lyric"),
                        PlayerLyricLineUi(2_000, "Intermediate target lyric"),
                        PlayerLyricLineUi(3_000, "Intermediate following lyric"),
                    ),
                    synchronizedLyrics = true,
                ),
            )
        composeRule.setContent {
            XyMusicTheme(dynamicColor = false) {
                Box(
                    modifier =
                    Modifier
                        .size(width = 480.dp, height = 300.dp)
                        .testTag(LYRICS_PANE_TAG),
                ) {
                    LyricsContent(
                        uiState = uiState.value,
                        onSeek = {},
                        compact = true,
                        centerActiveLine = true,
                    )
                }
            }
        }
        composeRule.waitForIdle()

        composeRule.onNodeWithText("Intermediate target lyric").performClick()
        composeRule.waitForIdle()

        composeRule.runOnIdle {
            uiState.value = uiState.value.copy(
                player = uiState.value.player.copy(positionMs = 1_000),
            )
        }
        composeRule.waitForIdle()
        val paneCenter =
            composeRule
                .onNodeWithTag(LYRICS_PANE_TAG)
                .fetchSemanticsNode()
                .boundsInRoot
                .center.y
        assertThat(abs(intermediateSecondLineCenter() - paneCenter)).isGreaterThan(2f)

        composeRule.runOnIdle {
            uiState.value = uiState.value.copy(
                player = uiState.value.player.copy(positionMs = 2_000),
            )
        }
        composeRule.waitForIdle()

        assertThat(abs(intermediateTargetLineCenter() - paneCenter)).isLessThan(2f)
        composeRule.onNodeWithText("Intermediate target lyric").assertIsSelected()
    }

    @Test
    fun clickingTheCurrentLineDoesNotBlockTheNextTransition() {
        val uiState =
            mutableStateOf(
                PlayerUiState(
                    player = PlayerState(positionMs = 0),
                    lyrics =
                    listOf(
                        PlayerLyricLineUi(0, "Current lyric"),
                        PlayerLyricLineUi(1_000, "Next lyric"),
                    ),
                    synchronizedLyrics = true,
                ),
            )
        composeRule.setContent {
            XyMusicTheme(dynamicColor = false) {
                Box(modifier = Modifier.size(width = 480.dp, height = 300.dp)) {
                    LyricsContent(
                        uiState = uiState.value,
                        onSeek = { position ->
                            uiState.value = uiState.value.copy(
                                player = uiState.value.player.copy(positionMs = position),
                            )
                        },
                        compact = true,
                    )
                }
            }
        }
        composeRule.waitForIdle()

        composeRule.onNodeWithText("Current lyric").performClick()
        composeRule.waitForIdle()
        composeRule.runOnIdle {
            uiState.value = uiState.value.copy(
                player = uiState.value.player.copy(positionMs = 1_000),
            )
        }
        composeRule.waitForIdle()

        composeRule.onNodeWithText("Next lyric").assertIsSelected()
    }

    @Test
    fun wordTimedHighlightOnlyAppliesToCurrentLineAndResetsPreviousLine() {
        val uiState =
            mutableStateOf(
                PlayerUiState(
                    player = PlayerState(positionMs = 0, durationMs = 2_000),
                    lyrics =
                    listOf(
                        PlayerLyricLineUi(
                            0,
                            "AB",
                            words = listOf(PlayerLyricWordUi(0, "A"), PlayerLyricWordUi(500, "B")),
                        ),
                        PlayerLyricLineUi(
                            1_000,
                            "CD",
                            words = listOf(PlayerLyricWordUi(1_000, "C"), PlayerLyricWordUi(1_500, "D")),
                        ),
                    ),
                    synchronizedLyrics = true,
                    lyricsTiming = com.xymusic.app.core.model.media.LyricsTiming.WORD,
                ),
            )
        composeRule.setContent {
            XyMusicTheme(dynamicColor = false) {
                Box(modifier = Modifier.size(width = 480.dp, height = 300.dp)) {
                    LyricsContent(
                        uiState = uiState.value,
                        onSeek = {},
                        compact = true,
                    )
                }
            }
        }

        composeRule.onNodeWithText("AB").assertIsSelected()
        composeRule.onNodeWithText("CD").assertIsNotSelected()

        composeRule.runOnIdle {
            uiState.value =
                uiState.value.copy(
                    player = uiState.value.player.copy(positionMs = 1_000),
                )
        }
        composeRule.waitForIdle()

        composeRule.onNodeWithText("AB").assertIsNotSelected()
        composeRule.onNodeWithText("CD").assertIsSelected()
    }

    @Test
    fun suppliedPlaybackPositionDrivesTheActiveLine() {
        val playbackPosition = mutableFloatStateOf(0f)
        val uiState =
            PlayerUiState(
                player = PlayerState(positionMs = 0, durationMs = 2_000),
                lyrics =
                listOf(
                    PlayerLyricLineUi(0, "Shared position first lyric"),
                    PlayerLyricLineUi(1_000, "Shared position second lyric"),
                ),
                synchronizedLyrics = true,
            )
        composeRule.setContent {
            XyMusicTheme(dynamicColor = false) {
                Box(modifier = Modifier.size(width = 480.dp, height = 300.dp)) {
                    LyricsContent(
                        uiState = uiState,
                        onSeek = {},
                        playbackPosition = playbackPosition,
                        compact = true,
                    )
                }
            }
        }

        composeRule.onNodeWithText("Shared position first lyric").assertIsSelected()
        composeRule.onNodeWithText("Shared position second lyric").assertIsNotSelected()

        composeRule.runOnIdle { playbackPosition.floatValue = 1_000f }
        composeRule.waitForIdle()

        composeRule.onNodeWithText("Shared position first lyric").assertIsNotSelected()
        composeRule.onNodeWithText("Shared position second lyric").assertIsSelected()
    }

    private fun secondLineCenter(): Float = composeRule
        .onNodeWithText("Second animated lyric")
        .fetchSemanticsNode()
        .boundsInRoot
        .center.y

    private fun lineCenter(text: String): Float = composeRule
        .onNodeWithText(text)
        .fetchSemanticsNode()
        .boundsInRoot
        .center.y

    private fun lineBounds(text: String) = composeRule
        .onNodeWithText(text)
        .fetchSemanticsNode()
        .boundsInRoot

    private fun targetLineCenter(): Float = composeRule
        .onNodeWithText("Seek target lyric")
        .fetchSemanticsNode()
        .boundsInRoot
        .center.y

    private fun followingLineCenter(): Float = composeRule
        .onNodeWithText("Seek following lyric")
        .fetchSemanticsNode()
        .boundsInRoot
        .center.y

    private fun intermediateTargetLineCenter(): Float = composeRule
        .onNodeWithText("Intermediate target lyric")
        .fetchSemanticsNode()
        .boundsInRoot
        .center.y

    private fun intermediateSecondLineCenter(): Float = composeRule
        .onNodeWithText("Intermediate second lyric")
        .fetchSemanticsNode()
        .boundsInRoot
        .center.y

    private companion object {
        const val LYRICS_PANE_TAG = "player_lyrics_animation_pane"
    }
}
