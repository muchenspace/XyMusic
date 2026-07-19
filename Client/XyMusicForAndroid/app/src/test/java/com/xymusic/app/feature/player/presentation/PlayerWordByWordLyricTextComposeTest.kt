package com.xymusic.app.feature.player.presentation

import androidx.compose.foundation.layout.width
import androidx.compose.runtime.MutableFloatState
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMusicTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(
    sdk = [34],
    application = ComposeTestApplication::class,
)
class PlayerWordByWordLyricTextComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun highlightOverlayDoesNotDuplicateSemanticsOrRecomposeForPositionUpdates() {
        lateinit var playbackPosition: MutableFloatState
        var compositionCount = 0
        val text = "逐字歌词"

        composeRule.setContent {
            XyMusicTheme(dynamicColor = false) {
                playbackPosition = remember { mutableFloatStateOf(1_000f) }
                SideEffect { compositionCount++ }
                WordByWordLyricText(
                    text = text,
                    highlightEndOffsets = listOf(1, 2, 3, 4),
                    playbackPosition = playbackPosition,
                    lineStartTimeMs = 1_000,
                    lineEndTimeMs = 3_000,
                    baseColor = Color.Gray,
                    highlightColor = Color.White,
                    style = TextStyle(fontSize = 24.sp, lineHeight = 32.sp),
                )
            }
        }
        composeRule.waitForIdle()

        composeRule
            .onAllNodesWithText(text, useUnmergedTree = true)
            .assertCountEquals(1)
        composeRule.runOnIdle {
            assertThat(compositionCount).isEqualTo(1)
            playbackPosition.floatValue = 1_750f
        }
        composeRule.waitForIdle()
        composeRule.runOnIdle {
            assertThat(compositionCount).isEqualTo(1)
        }
    }

    @Test
    fun rightToLeftTextCanWrapAcrossMultipleLines() {
        val text = "שלום עולם ארוך מאוד"

        composeRule.setContent {
            XyMusicTheme(dynamicColor = false) {
                WordByWordLyricText(
                    text = text,
                    highlightEndOffsets = (1..text.length).toList(),
                    playbackPosition = remember { mutableFloatStateOf(2_000f) },
                    lineStartTimeMs = 1_000,
                    lineEndTimeMs = 3_000,
                    modifier = Modifier.width(64.dp),
                    baseColor = Color.Gray,
                    highlightColor = Color.White,
                    style = TextStyle(fontSize = 24.sp, lineHeight = 30.sp),
                )
            }
        }
        composeRule.waitForIdle()

        val bounds = composeRule.onNodeWithText(text).fetchSemanticsNode().boundsInRoot
        assertThat(bounds.height).isGreaterThan(bounds.width)
    }
}
