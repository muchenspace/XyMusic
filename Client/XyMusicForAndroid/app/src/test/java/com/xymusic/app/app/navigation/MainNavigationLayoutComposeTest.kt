package com.xymusic.app.app.navigation

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.click
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performTouchInput
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMusicTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ComposeTestApplication::class, qualifiers = "w360dp-h640dp")
class MainNavigationLayoutComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun primaryContentChangesInsetAtTheChromeStateBoundaryOnly() {
        val config = phoneConfig()
        var chromeState by mutableStateOf(homeChrome())
        val contentHeights = mutableListOf<Int>()

        composeRule.setContent {
            XyMusicTheme(dynamicColor = false) {
                MainNavigationLayout(
                    config = config,
                    chromeState = chromeState,
                    snackbarHostState = remember { SnackbarHostState() },
                    navigationRail = {},
                    bottomNavigation = { Box(Modifier.fillMaxWidth().height(MainNavigationBarHeight)) },
                    miniPlayer = { modifier ->
                        Box(modifier.fillMaxWidth().height(config.miniPlayerHeight))
                    },
                ) { chromeInsets ->
                    MainNavigationRouteLayout(
                        layout = MainNavigationContentLayout.Primary,
                        config = config,
                        chromeInsets = chromeInsets,
                    ) {
                        Box(
                            Modifier
                                .fillMaxSize()
                                .onSizeChanged { contentHeights += it.height }
                                .testTag(PRIMARY_CONTENT_TAG),
                        )
                    }
                }
            }
        }
        composeRule.waitForIdle()

        composeRule.mainClock.autoAdvance = false
        composeRule.runOnIdle { chromeState = secondaryChrome() }
        repeat(8) {
            composeRule.mainClock.advanceTimeBy(40L)
            composeRule.waitForIdle()
        }
        composeRule.mainClock.autoAdvance = true

        assertThat(contentHeights.distinct().size).isAtMost(2)
        assertThat(contentHeights.last()).isGreaterThan(contentHeights.first())
    }

    @Test
    fun miniPlayerMovesContinuouslyWhenBottomNavigationLeaves() {
        val config = phoneConfig()
        var chromeState by mutableStateOf(homeChrome())

        composeRule.setContent {
            XyMusicTheme(dynamicColor = false) {
                MainNavigationLayout(
                    config = config,
                    chromeState = chromeState,
                    snackbarHostState = remember { SnackbarHostState() },
                    navigationRail = {},
                    bottomNavigation = { Box(Modifier.fillMaxWidth().height(MainNavigationBarHeight)) },
                    miniPlayer = { modifier ->
                        Box(
                            modifier
                                .fillMaxWidth()
                                .height(config.miniPlayerHeight)
                                .testTag(MINI_PLAYER_TAG),
                        )
                    },
                ) { chromeInsets ->
                    MainNavigationRouteLayout(
                        layout = MainNavigationContentLayout.Secondary,
                        config = config,
                        chromeInsets = chromeInsets,
                    ) {
                        Box(Modifier.fillMaxSize())
                    }
                }
            }
        }
        composeRule.waitForIdle()

        val positions = mutableListOf(miniPlayerTop())
        composeRule.mainClock.autoAdvance = false
        composeRule.runOnIdle { chromeState = secondaryChrome() }
        repeat(8) {
            composeRule.mainClock.advanceTimeBy(40L)
            composeRule.waitForIdle()
            positions += miniPlayerTop()
        }
        composeRule.mainClock.autoAdvance = true

        positions.zipWithNext().forEach { (previous, next) ->
            assertThat(next + POSITION_TOLERANCE_PX).isAtLeast(previous)
        }
        assertThat(positions.last()).isGreaterThan(positions.first() + POSITION_TOLERANCE_PX)
    }

    @Test
    fun chromeReceivesTouchAboveNavigationContent() {
        val config = phoneConfig()
        var miniPlayerClicks = 0
        var contentClicks = 0

        composeRule.setContent {
            XyMusicTheme(dynamicColor = false) {
                MainNavigationLayout(
                    config = config,
                    chromeState = homeChrome(),
                    snackbarHostState = remember { SnackbarHostState() },
                    navigationRail = {},
                    bottomNavigation = {},
                    miniPlayer = { modifier ->
                        Box(
                            modifier
                                .fillMaxWidth()
                                .height(config.miniPlayerHeight)
                                .clickable { miniPlayerClicks++ }
                                .testTag(MINI_PLAYER_TAG),
                        )
                    },
                ) { _ ->
                    Box(
                        Modifier
                            .fillMaxSize()
                            .clickable { contentClicks++ }
                            .testTag(CONTENT_TAG),
                    )
                }
            }
        }

        composeRule.onNodeWithTag(MINI_PLAYER_TAG).performTouchInput { click() }

        composeRule.runOnIdle {
            assertThat(miniPlayerClicks).isEqualTo(1)
            assertThat(contentClicks).isEqualTo(0)
        }
    }

    private fun miniPlayerTop(): Float =
        composeRule.onNodeWithTag(MINI_PLAYER_TAG).fetchSemanticsNode().boundsInRoot.top

    private fun phoneConfig() = MainNavigationLayoutConfig(
        useNavigationRail = false,
        compactPlayerBar = false,
        hasPlayerItem = true,
    )

    private fun homeChrome() = MainNavigationChromeState(
        showMainNavigation = true,
        showMiniPlayer = true,
        selectedMainDestination = MainDestination.Home,
    )

    private fun secondaryChrome() = MainNavigationChromeState(
        showMainNavigation = false,
        showMiniPlayer = true,
        selectedMainDestination = MainDestination.Home,
    )

    private companion object {
        const val PRIMARY_CONTENT_TAG = "main_navigation_primary_content"
        const val MINI_PLAYER_TAG = "main_navigation_mini_player"
        const val CONTENT_TAG = "main_navigation_content"
        const val POSITION_TOLERANCE_PX = 0.5f
    }
}
