package com.xymusic.app.app.navigation

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.test.swipeDown
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMotion
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
    qualifiers = "w360dp-h640dp",
)
class PlayerTransitionOverlayComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun openingOverlayEntersFromBelowTheScreenWithoutReversing() {
        val fixture = setFixture()
        val homeBounds = homeBounds()

        composeRule.mainClock.autoAdvance = false
        startOpening(fixture)

        val positions = mutableListOf(overlayTop())
        repeat(OPENING_SAMPLE_COUNT) {
            composeRule.mainClock.advanceTimeBy(OPENING_SAMPLE_MILLIS)
            composeRule.waitForIdle()
            positions += overlayTop()
        }
        composeRule.mainClock.autoAdvance = true
        composeRule.waitForIdle()

        assertThat(positions.first()).isAtLeast(homeBounds.bottom - POSITION_TOLERANCE_PX)
        assertThat(positions.last()).isLessThan(positions.first() - POSITION_TOLERANCE_PX)
        assertMonotonicallyNonIncreasing(positions)
    }

    @Test
    fun openingAnimationUpdatesTheLayerWithoutRecomposingPlayerContentPerFrame() {
        val fixture = setFixture()

        composeRule.mainClock.autoAdvance = false
        startOpening(fixture)
        val compositionsAfterEntryStarted = fixture.contentCompositionCount()

        repeat(OPENING_RECOMPOSITION_SAMPLE_COUNT) {
            composeRule.mainClock.advanceTimeBy(OPENING_RECOMPOSITION_SAMPLE_MILLIS)
            composeRule.waitForIdle()
        }

        val animationCompositions = fixture.contentCompositionCount() - compositionsAfterEntryStarted
        assertThat(animationCompositions).isAtMost(1)
        composeRule.mainClock.autoAdvance = true
        composeRule.waitForIdle()
    }

    @Test
    fun closingOverlayLeavesFromItsCurrentPositionWhileHomeStaysStable() {
        val fixture = setFixture()
        val homeBounds = homeBounds()

        composeRule.mainClock.autoAdvance = false
        startOpening(fixture)
        composeRule.mainClock.advanceTimeBy(OPENING_PROGRESS_MILLIS)
        composeRule.waitForIdle()

        val topAtDismissRequest = overlayTop()
        fixture.requestDismiss()
        composeRule.waitForIdle()

        val exitPositions = mutableListOf(overlayTop())
        assertHomeRemainsStable(homeBounds)
        repeat(CLOSING_SAMPLE_COUNT) {
            composeRule.mainClock.advanceTimeBy(CLOSING_SAMPLE_MILLIS)
            composeRule.waitForIdle()
            exitPositions += overlayTop()
            assertHomeRemainsStable(homeBounds)
        }
        composeRule.mainClock.autoAdvance = true
        composeRule.waitForIdle()

        assertHomeRemainsStable(homeBounds)
        assertThat(exitPositions.first()).isWithin(POSITION_TOLERANCE_PX).of(topAtDismissRequest)
        assertThat(exitPositions.last()).isGreaterThan(exitPositions.first() + POSITION_TOLERANCE_PX)
        assertMonotonicallyNonDecreasing(exitPositions)
    }

    @Test
    fun overlayIsRemovedAfterItsClosingAnimationCompletes() {
        val fixture = setFixture()

        composeRule.mainClock.autoAdvance = false
        startOpening(fixture)
        composeRule.mainClock.advanceTimeBy(XyMotion.Emphasized.toLong() + SETTLE_MILLIS)
        composeRule.waitForIdle()

        fixture.requestDismiss()
        composeRule.waitForIdle()
        composeRule.mainClock.advanceTimeBy(XyMotion.Standard.toLong() + SETTLE_MILLIS)
        composeRule.mainClock.autoAdvance = true
        composeRule.waitForIdle()

        composeRule.onNodeWithTag(PlayerTransitionTestTags.Surface).assertDoesNotExist()
        composeRule.onNodeWithTag(HOME_TAG).assertExists()
    }

    @Test
    fun thresholdDragDismissesWithoutResettingTheSurfaceToTheTop() {
        val fixture = setFixture()
        val homeBounds = homeBounds()

        composeRule.mainClock.autoAdvance = false
        startOpening(fixture)
        composeRule.mainClock.advanceTimeBy(XyMotion.Emphasized.toLong() + SETTLE_MILLIS)
        composeRule.waitForIdle()

        composeRule.onNodeWithTag(PLAYER_TAG).performTouchInput { swipeDown() }
        composeRule.waitForIdle()

        val topAfterDismissRequest = overlayTop()
        assertThat(fixture.dismissRequestCount()).isEqualTo(1)
        assertThat(topAfterDismissRequest).isGreaterThan(homeBounds.height * MINIMUM_DRAG_FRACTION)

        composeRule.mainClock.advanceTimeByFrame()
        composeRule.waitForIdle()
        assertThat(overlayTop()).isAtLeast(topAfterDismissRequest - POSITION_TOLERANCE_PX)

        composeRule.mainClock.autoAdvance = true
        composeRule.waitForIdle()
    }

    private fun setFixture(): OverlayFixture {
        lateinit var setVisible: (Boolean) -> Unit
        lateinit var requestDismiss: () -> Unit
        lateinit var dismissRequestCount: () -> Int
        var contentCompositionCount = 0

        composeRule.setContent {
            XyMusicTheme(dynamicColor = false) {
                var visible by remember { mutableStateOf(false) }
                var dismissRequests by remember { mutableStateOf(0) }
                setVisible = { visible = it }
                dismissRequestCount = { dismissRequests }

                Box(modifier = Modifier.fillMaxSize().testTag(ROOT_TAG)) {
                    Box(modifier = Modifier.fillMaxSize().testTag(HOME_TAG))
                    PlayerTransitionOverlay(
                        visible = visible,
                        onDismissRequest = {
                            dismissRequests++
                            visible = false
                        },
                    ) { onDismiss, dismissGestureModifier, _ ->
                        requestDismiss = onDismiss
                        SideEffect { contentCompositionCount += 1 }
                        Box(
                            modifier =
                            dismissGestureModifier
                                .fillMaxSize()
                                .testTag(PLAYER_TAG),
                        )
                    }
                }
            }
        }
        composeRule.waitForIdle()

        return OverlayFixture(
            setVisible = { visible -> composeRule.runOnIdle { setVisible(visible) } },
            requestDismiss = { composeRule.runOnIdle { requestDismiss() } },
            dismissRequestCount = { composeRule.runOnIdle { dismissRequestCount() } },
            contentCompositionCount = { composeRule.runOnIdle { contentCompositionCount } },
        )
    }

    private fun startOpening(fixture: OverlayFixture) {
        fixture.setVisible(true)
        repeat(INITIAL_COMPOSITION_FRAME_COUNT) {
            composeRule.mainClock.advanceTimeByFrame()
            composeRule.waitForIdle()
        }
    }

    private fun overlayTop(): Float = composeRule
        .onNodeWithTag(PlayerTransitionTestTags.Surface)
        .fetchSemanticsNode()
        .boundsInRoot
        .top

    private fun homeBounds(): Rect = composeRule.onNodeWithTag(HOME_TAG).fetchSemanticsNode().boundsInRoot

    private fun assertHomeRemainsStable(expectedBounds: Rect) {
        composeRule.onNodeWithTag(HOME_TAG).assertExists()
        assertThat(homeBounds()).isEqualTo(expectedBounds)
    }

    private fun assertMonotonicallyNonIncreasing(positions: List<Float>) {
        positions.zipWithNext().forEach { (previous, next) ->
            assertThat(next).isAtMost(previous + POSITION_TOLERANCE_PX)
        }
    }

    private fun assertMonotonicallyNonDecreasing(positions: List<Float>) {
        positions.zipWithNext().forEach { (previous, next) ->
            assertThat(next).isAtLeast(previous - POSITION_TOLERANCE_PX)
        }
    }

    private data class OverlayFixture(
        val setVisible: (Boolean) -> Unit,
        val requestDismiss: () -> Unit,
        val dismissRequestCount: () -> Int,
        val contentCompositionCount: () -> Int,
    )

    private companion object {
        const val OPENING_SAMPLE_COUNT = 7
        const val OPENING_SAMPLE_MILLIS = 40L
        const val OPENING_RECOMPOSITION_SAMPLE_COUNT = 5
        const val OPENING_RECOMPOSITION_SAMPLE_MILLIS = 32L
        const val OPENING_PROGRESS_MILLIS = 120L
        const val CLOSING_SAMPLE_COUNT = 5
        const val CLOSING_SAMPLE_MILLIS = 32L
        const val SETTLE_MILLIS = 100L
        const val POSITION_TOLERANCE_PX = 0.5f
        const val MINIMUM_DRAG_FRACTION = 0.1f
        const val INITIAL_COMPOSITION_FRAME_COUNT = 2

        const val ROOT_TAG = "player_transition_fixture_root"
        const val HOME_TAG = "player_transition_fixture_home"
        const val PLAYER_TAG = "player_transition_fixture_player"
    }
}
