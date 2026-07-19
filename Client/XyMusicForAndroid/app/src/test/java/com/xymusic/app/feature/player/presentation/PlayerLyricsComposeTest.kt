package com.xymusic.app.feature.player.presentation

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.mutableStateOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsNotSelected
import androidx.compose.ui.test.assertIsSelected
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
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

        composeRule.mainClock.advanceTimeBy(210)
        composeRule.waitForIdle()
        val midwayDistance = abs(secondLineCenter() - paneCenter)
        assertThat(midwayDistance).isLessThan(transitionStartDistance)
        assertThat(midwayDistance).isGreaterThan(2f)

        composeRule.mainClock.advanceTimeBy(300)
        composeRule.mainClock.autoAdvance = true
        composeRule.waitForIdle()
        assertThat(abs(secondLineCenter() - paneCenter)).isLessThan(2f)
    }

    @Test
    fun wordByWordHighlightOnlyAppliesToCurrentLineAndResetsPreviousLine() {
        val uiState =
            mutableStateOf(
                PlayerUiState(
                    player = PlayerState(positionMs = 0, durationMs = 2_000),
                    lyrics =
                    listOf(
                        PlayerLyricLineUi(0, "AB", highlightEndOffsets = listOf(1, 2)),
                        PlayerLyricLineUi(1_000, "CD", highlightEndOffsets = listOf(1, 2)),
                    ),
                    synchronizedLyrics = true,
                    wordByWordLyricsEnabled = true,
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

    private fun secondLineCenter(): Float = composeRule
        .onNodeWithText("Second animated lyric")
        .fetchSemanticsNode()
        .boundsInRoot
        .center.y

    private companion object {
        const val LYRICS_PANE_TAG = "player_lyrics_animation_pane"
    }
}
