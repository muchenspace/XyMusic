package com.xymusic.app.feature.player.presentation

import androidx.compose.foundation.layout.size
import androidx.compose.runtime.MutableFloatState
import androidx.compose.runtime.Recomposer
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.layout
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.unit.dp
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.player.domain.model.PlayerState
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMusicTheme
import kotlinx.coroutines.flow.MutableStateFlow
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
class PlayerLyricsHighRefreshComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun playbackClockStartsAtTheCurrentPositionOnItsFirstComposition() {
        val playerFlow = MutableStateFlow(PlayerState(positionMs = 48_765L))
        var firstCompositionValue = Float.NaN

        composeRule.setContent {
            val playbackPosition = rememberPlaybackPositionState(playerFlow)
            if (firstCompositionValue.isNaN()) firstCompositionValue = playbackPosition.value
        }

        assertThat(firstCompositionValue).isEqualTo(48_765f)
    }

    /**
     * Position is intentionally read by the draw modifier in production. This test publishes
     * samples spaced at 120 Hz and verifies that they apply no composition changes and trigger no
     * measure or placement work. Actual 120/144 Hz frame pacing belongs in a device benchmark.
     */
    @Test
    fun positionSamplesDoNotApplyCompositionChangesOrRelayoutTheWordLyricTree() {
        lateinit var playbackPosition: MutableFloatState
        var measureCount = 0
        var placementCount = 0
        val text = "Smooth word highlight"
        val words = listOf(
            PlayerLyricWordUi(0, "Smooth ", endTimeMs = 1_000),
            PlayerLyricWordUi(1_000, "word ", endTimeMs = 2_000),
            PlayerLyricWordUi(2_000, "highlight", endTimeMs = 4_000),
        )

        composeRule.setContent {
            XyMusicTheme(dynamicColor = false) {
                playbackPosition = remember { mutableFloatStateOf(100f) }
                WordByWordLyricText(
                    text = text,
                    words = words,
                    playbackPosition = playbackPosition,
                    modifier = Modifier
                        .size(width = 360.dp, height = 96.dp)
                        .layout { measurable, constraints ->
                            measureCount += 1
                            val placeable = measurable.measure(constraints)
                            layout(placeable.width, placeable.height) {
                                placementCount += 1
                                placeable.place(0, 0)
                            }
                        },
                    baseColor = androidx.compose.ui.graphics.Color.Gray,
                    highlightColor = androidx.compose.ui.graphics.Color.White,
                    style = androidx.compose.material3.LocalTextStyle.current,
                    lineEmphasis = remember { mutableFloatStateOf(1f) },
                )
            }
        }
        composeRule.waitForIdle()
        val (recomposerInfo, initialAppliedChangeCount) =
            composeRule.runOnIdle {
                val running = Recomposer.runningRecomposers.value
                assertThat(running).hasSize(1)
                running.single().let { info -> info to info.changeCount }
            }
        val initialMeasureCount = measureCount
        val initialPlacementCount = placementCount

        repeat(24) { sample ->
            composeRule.runOnIdle {
                playbackPosition.floatValue = 100f + (sample + 1) * (1_000f / 120f)
            }
            composeRule.waitForIdle()
        }

        composeRule.runOnIdle {
            assertThat(Recomposer.runningRecomposers.value).containsExactly(recomposerInfo)
            assertThat(recomposerInfo.changeCount).isEqualTo(initialAppliedChangeCount)
            assertThat(measureCount).isEqualTo(initialMeasureCount)
            assertThat(placementCount).isEqualTo(initialPlacementCount)
        }
    }
}
