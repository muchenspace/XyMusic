package com.xymusic.app.app.navigation

import androidx.compose.animation.ExitTransition
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.click
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.unit.IntSize
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.playlist.presentation.PlaylistRouteArgs
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMotion
import com.xymusic.app.ui.theme.XyMusicTheme
import com.xymusic.app.ui.theme.playerReturnInto
import com.xymusic.app.ui.theme.playerSlideInto
import com.xymusic.app.ui.theme.slideFadeBackInto
import com.xymusic.app.ui.theme.slideFadeBackOutOf
import com.xymusic.app.ui.theme.slideFadeInto
import com.xymusic.app.ui.theme.slideFadeOutOf
import kotlin.math.roundToInt
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ComposeTestApplication::class)
class MainNavigationLayoutComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    @Config(qualifiers = PhoneQualifiers)
    fun homeWithMiniPlayerToPlayerKeepsPhoneNavigationHostStable() {
        verifyTransition(
            config =
            MainNavigationLayoutConfig(
                useNavigationRail = false,
                compactPlayerBar = false,
                hasPlayerItem = true,
            ),
            destinationRoute = PlayerDestination.NowPlaying.route,
            durationMillis = XyMotion.Emphasized,
            target = FixtureTarget.Player,
            initialChrome = PhoneHomeWithMiniChrome,
            inFlightChrome = PhoneHomeWithMiniChrome,
            finalChrome = HiddenChrome,
        )
    }

    @Test
    @Config(qualifiers = PhoneQualifiers)
    fun playerPopKeepsThePlayerComposedUntilTheReturnTransitionCompletes() {
        val fixture =
            setFixture(
                MainNavigationLayoutConfig(
                    useNavigationRail = false,
                    compactPlayerBar = false,
                    hasPlayerItem = true,
                ),
            )

        composeRule.mainClock.autoAdvance = false
        composeRule.runOnIdle {
            fixture.navController.navigate(PlayerDestination.NowPlaying.route)
        }
        composeRule.mainClock.advanceTimeBy(XyMotion.Emphasized + TransitionSettleMillis)
        composeRule.waitForIdle()

        composeRule.runOnIdle { fixture.navController.navigateUp() }
        composeRule.mainClock.advanceTimeByFrame()
        composeRule.waitForIdle()
        composeRule.mainClock.advanceTimeByFrame()
        composeRule.waitForIdle()

        composeRule.onNodeWithTag(PlayerTag).assertExists()
        composeRule.onNodeWithTag(HomeTag).assertExists()

        composeRule.mainClock.advanceTimeBy(XyMotion.Slow + TransitionSettleMillis)
        composeRule.mainClock.autoAdvance = true
        composeRule.waitForIdle()

        composeRule.onNodeWithTag(PlayerTag).assertDoesNotExist()
        composeRule.onNodeWithTag(HomeTag).assertExists()
    }

    @Test
    @Config(qualifiers = PhoneQualifiers)
    fun homeWithoutMiniPlayerToPlaylistKeepsPhoneNavigationHostStable() {
        verifyTransition(
            config =
            MainNavigationLayoutConfig(
                useNavigationRail = false,
                compactPlayerBar = false,
                hasPlayerItem = false,
            ),
            destinationRoute = PlaylistDestination.Detail.createRoute(FixturePlaylistId),
            durationMillis = XyMotion.Standard,
            target = FixtureTarget.Playlist,
            initialChrome = PhoneHomeWithoutMiniChrome,
            inFlightChrome = PhoneHomeWithoutMiniChrome,
            finalChrome = HiddenChrome,
        )
    }

    @Test
    @Config(qualifiers = PhoneQualifiers)
    fun homeWithMiniPlayerToPlaylistKeepsPhoneNavigationHostStable() {
        verifyTransition(
            config =
            MainNavigationLayoutConfig(
                useNavigationRail = false,
                compactPlayerBar = false,
                hasPlayerItem = true,
            ),
            destinationRoute = PlaylistDestination.Detail.createRoute(FixturePlaylistId),
            durationMillis = XyMotion.Standard,
            target = FixtureTarget.Playlist,
            initialChrome = PhoneHomeWithMiniChrome,
            inFlightChrome = PhoneHomeWithMiniChrome,
            finalChrome = PlaylistWithMiniChrome,
        )
    }

    @Test
    @Config(qualifiers = PhoneQualifiers)
    fun homeToPlaylistMovesMiniPlayerDownWithoutAReflowJump() {
        val fixture =
            setFixture(
                MainNavigationLayoutConfig(
                    useNavigationRail = false,
                    compactPlayerBar = false,
                    hasPlayerItem = true,
                ),
            )
        val topPositions = mutableListOf(miniPlayerTop())

        composeRule.mainClock.autoAdvance = false
        composeRule.runOnIdle {
            fixture.navController.navigate(PlaylistDestination.Detail.createRoute(FixturePlaylistId))
        }
        repeat(10) {
            composeRule.mainClock.advanceTimeBy(32L)
            composeRule.waitForIdle()
            topPositions += miniPlayerTop()
        }
        composeRule.mainClock.autoAdvance = true
        composeRule.waitForIdle()

        topPositions.zipWithNext().forEach { (previous, next) ->
            assertThat(next + PositionTolerancePx).isAtLeast(previous)
        }
        assertThat(topPositions.last()).isGreaterThan(topPositions.first() + PositionTolerancePx)
    }

    @Test
    @Config(qualifiers = CompactLandscapeQualifiers)
    fun homeWithMiniPlayerToPlayerKeepsLandscapeNavigationHostStable() {
        verifyTransition(
            config =
            MainNavigationLayoutConfig(
                useNavigationRail = true,
                compactPlayerBar = true,
                hasPlayerItem = true,
            ),
            destinationRoute = PlayerDestination.NowPlaying.route,
            durationMillis = XyMotion.Emphasized,
            target = FixtureTarget.Player,
            initialChrome = RailHomeWithMiniChrome,
            inFlightChrome = RailHomeWithMiniChrome,
            finalChrome = HiddenChrome,
        )
    }

    @Test
    @Config(qualifiers = CompactLandscapeQualifiers)
    fun landscapeChromeReceivesTouchAboveNavigationContent() {
        val config =
            MainNavigationLayoutConfig(
                useNavigationRail = true,
                compactPlayerBar = true,
                hasPlayerItem = true,
            )
        var railClicks = 0
        var miniPlayerClicks = 0
        var contentClicks = 0

        composeRule.setContent {
            XyMusicTheme(dynamicColor = false) {
                MainNavigationLayout(
                    config = config,
                    chromeState =
                    MainNavigationChromeState(
                        showMainNavigation = true,
                        showMiniPlayer = true,
                        selectedMainDestination = MainDestination.Home,
                        isPlayerDestination = false,
                    ),
                    playerEntryStillVisible = false,
                    snackbarHostState = remember { SnackbarHostState() },
                    navigationRail = {
                        Box(
                            modifier =
                            Modifier
                                .width(MainNavigationRailWidth)
                                .fillMaxHeight()
                                .clickable { railClicks++ }
                                .testTag(NavigationRailTouchTag),
                        )
                    },
                    bottomNavigation = {},
                    miniPlayer = { modifier ->
                        Box(
                            modifier =
                            modifier
                                .fillMaxWidth()
                                .height(config.miniPlayerHeight)
                                .clickable { miniPlayerClicks++ }
                                .testTag(MiniPlayerTouchTag),
                        )
                    },
                ) {
                    Box(
                        modifier =
                        Modifier
                            .fillMaxSize()
                            .clickable { contentClicks++ }
                            .testTag(NavigationContentTouchTag),
                    )
                }
            }
        }

        composeRule.onNodeWithTag(NavigationRailTouchTag).performTouchInput { click() }
        composeRule.onNodeWithTag(MiniPlayerTouchTag).performTouchInput { click() }

        composeRule.runOnIdle {
            assertThat(railClicks).isEqualTo(1)
            assertThat(miniPlayerClicks).isEqualTo(1)
            assertThat(contentClicks).isEqualTo(0)
        }
    }

    private fun verifyTransition(
        config: MainNavigationLayoutConfig,
        destinationRoute: String,
        durationMillis: Int,
        target: FixtureTarget,
        initialChrome: ChromeExpectation,
        inFlightChrome: ChromeExpectation,
        finalChrome: ChromeExpectation,
    ) {
        val fixture = setFixture(config)
        val initialHostBounds = navigationHostBounds()
        val rootBounds = composeRule.onNodeWithTag(RootTag).fetchSemanticsNode().boundsInRoot
        val initialHomeSize = fixture.probe.homeSize

        assertThat(initialHostBounds).isEqualTo(rootBounds)
        assertThat(initialHomeSize).isNotNull()
        assertChrome(initialChrome)

        composeRule.mainClock.autoAdvance = false
        composeRule.runOnIdle {
            fixture.navController.navigate(destinationRoute)
        }
        composeRule.mainClock.advanceTimeByFrame()
        composeRule.waitForIdle()

        assertShellFrame(
            fixture = fixture,
            initialHostBounds = initialHostBounds,
            initialHomeSize = initialHomeSize,
            expectedChrome = inFlightChrome,
        )

        // NavHost commits the target entry one test-clock tick before measuring its content.
        composeRule.mainClock.advanceTimeByFrame()
        composeRule.waitForIdle()

        assertStableFrame(
            fixture = fixture,
            target = target,
            initialHostBounds = initialHostBounds,
            initialHomeSize = initialHomeSize,
            expectedChrome = inFlightChrome,
        )

        composeRule.mainClock.advanceTimeBy(durationMillis / 2L)
        composeRule.waitForIdle()

        assertStableFrame(
            fixture = fixture,
            target = target,
            initialHostBounds = initialHostBounds,
            initialHomeSize = initialHomeSize,
            expectedChrome = inFlightChrome,
        )

        composeRule.mainClock.advanceTimeBy(durationMillis + TransitionSettleMillis)
        composeRule.mainClock.autoAdvance = true
        composeRule.waitForIdle()

        assertThat(navigationHostBounds()).isEqualTo(initialHostBounds)
        assertTargetSize(fixture, target, initialHostBounds)
        assertChrome(finalChrome)
    }

    private fun setFixture(config: MainNavigationLayoutConfig): NavigationFixture {
        lateinit var navController: NavHostController
        val probe = LayoutProbe()

        composeRule.setContent {
            XyMusicTheme(dynamicColor = false) {
                navController = rememberNavController()
                val currentBackStackEntry by navController.currentBackStackEntryAsState()
                val visibleEntries by navController.visibleEntries.collectAsState()
                val currentRoute = currentBackStackEntry?.destination?.route
                val playerEntryStillVisible =
                    visibleEntries.any { entry -> entry.destination.route == PlayerDestination.NowPlaying.route }

                MainNavigationLayout(
                    config = config,
                    chromeState =
                    mainNavigationChromeState(
                        config = config,
                        currentRoute = currentRoute,
                        lastSelectedMainDestination = MainDestination.Home,
                    ),
                    playerEntryStillVisible = playerEntryStillVisible,
                    snackbarHostState = remember { SnackbarHostState() },
                    navigationRail = {
                        Box(
                            modifier =
                            Modifier
                                .width(MainNavigationRailWidth)
                                .fillMaxHeight()
                                .testTag(NavigationRailTag),
                        )
                    },
                    bottomNavigation = {
                        Box(
                            modifier =
                            Modifier
                                .fillMaxWidth()
                                .height(MainNavigationBarHeight)
                                .testTag(BottomNavigationTag),
                        )
                    },
                    miniPlayer = { modifier ->
                        Box(
                            modifier =
                            modifier
                                .fillMaxWidth()
                                .height(config.miniPlayerHeight)
                                .testTag(MiniPlayerTag),
                        )
                    },
                    modifier = Modifier.testTag(RootTag),
                ) {
                    FixtureNavHost(
                        navController = navController,
                        config = config,
                        probe = probe,
                    )
                }
            }
        }
        composeRule.waitForIdle()

        return NavigationFixture(navController = navController, probe = probe)
    }

    private fun assertStableFrame(
        fixture: NavigationFixture,
        target: FixtureTarget,
        initialHostBounds: Rect,
        initialHomeSize: IntSize?,
        expectedChrome: ChromeExpectation,
    ) {
        assertShellFrame(
            fixture = fixture,
            initialHostBounds = initialHostBounds,
            initialHomeSize = initialHomeSize,
            expectedChrome = expectedChrome,
        )
        assertTargetSize(fixture, target, initialHostBounds)
    }

    private fun assertShellFrame(
        fixture: NavigationFixture,
        initialHostBounds: Rect,
        initialHomeSize: IntSize?,
        expectedChrome: ChromeExpectation,
    ) {
        assertThat(navigationHostBounds()).isEqualTo(initialHostBounds)
        assertThat(fixture.probe.homeSize).isEqualTo(initialHomeSize)
        assertChrome(expectedChrome)
    }

    private fun assertTargetSize(fixture: NavigationFixture, target: FixtureTarget, navigationHostBounds: Rect) {
        if (target != FixtureTarget.Player) return

        assertThat(fixture.probe.playerSize)
            .isEqualTo(
                IntSize(
                    width = navigationHostBounds.width.roundToInt(),
                    height = navigationHostBounds.height.roundToInt(),
                ),
            )
    }

    private fun assertChrome(expectation: ChromeExpectation) {
        assertNodePresence(BottomNavigationTag, expectation.bottomNavigation)
        assertNodePresence(NavigationRailTag, expectation.navigationRail)
        assertNodePresence(MiniPlayerTag, expectation.miniPlayer)
    }

    private fun assertNodePresence(tag: String, expected: Boolean) {
        val node = composeRule.onNodeWithTag(tag)
        if (expected) {
            node.assertExists()
        } else {
            node.assertDoesNotExist()
        }
    }

    private fun navigationHostBounds(): Rect =
        composeRule.onNodeWithTag(NavigationHostTag).fetchSemanticsNode().boundsInRoot

    private fun miniPlayerTop(): Float = composeRule.onNodeWithTag(MiniPlayerTag).fetchSemanticsNode().boundsInRoot.top
}

@Composable
private fun FixtureNavHost(navController: NavHostController, config: MainNavigationLayoutConfig, probe: LayoutProbe) {
    NavHost(
        navController = navController,
        startDestination = MainDestination.Home.route,
        modifier =
        Modifier
            .fillMaxSize()
            .testTag(NavigationHostTag),
        enterTransition = {
            if (targetState.destination.route == PlayerDestination.NowPlaying.route) {
                playerSlideInto()
            } else {
                slideFadeInto()
            }
        },
        exitTransition = {
            if (targetState.destination.route == PlayerDestination.NowPlaying.route) {
                ExitTransition.None
            } else {
                slideFadeOutOf()
            }
        },
        popEnterTransition = {
            when {
                targetState.destination.route == PlayerDestination.NowPlaying.route -> playerSlideInto()
                initialState.destination.route == PlayerDestination.NowPlaying.route -> playerReturnInto()
                else -> slideFadeBackInto()
            }
        },
        popExitTransition = {
            if (initialState.destination.route == PlayerDestination.NowPlaying.route) {
                ExitTransition.KeepUntilTransitionsFinished
            } else {
                slideFadeBackOutOf()
            }
        },
    ) {
        composable(route = MainDestination.Home.route) {
            MainNavigationRouteLayout(
                layout = MainNavigationContentLayout.Primary,
                config = config,
            ) {
                FixturePage(
                    tag = HomeTag,
                    onSizeChanged = { size -> probe.homeSize = size },
                )
            }
        }
        composable(route = PlayerDestination.NowPlaying.route) {
            MainNavigationRouteLayout(
                layout = MainNavigationContentLayout.FullScreen,
                config = config,
            ) {
                FixturePage(
                    tag = PlayerTag,
                    onSizeChanged = { size -> probe.playerSize = size },
                )
            }
        }
        composable(
            route = PlaylistDestination.Detail.route,
            arguments =
            listOf(
                navArgument(PlaylistRouteArgs.PlaylistId) { type = NavType.StringType },
            ),
        ) {
            MainNavigationRouteLayout(
                layout = MainNavigationContentLayout.EdgeToEdge,
                config = config,
            ) {
                FixturePage(
                    tag = PlaylistTag,
                    onSizeChanged = { size -> probe.playlistSize = size },
                )
            }
        }
    }
}

@Composable
private fun FixturePage(tag: String, onSizeChanged: (IntSize) -> Unit) {
    Box(
        modifier =
        Modifier
            .fillMaxSize()
            .onSizeChanged(onSizeChanged)
            .testTag(tag),
    )
}

private data class NavigationFixture(val navController: NavHostController, val probe: LayoutProbe)

private class LayoutProbe {
    var homeSize: IntSize? = null
    var playerSize: IntSize? = null
    var playlistSize: IntSize? = null
}

private enum class FixtureTarget {
    Player,
    Playlist,
}

private data class ChromeExpectation(
    val bottomNavigation: Boolean = false,
    val navigationRail: Boolean = false,
    val miniPlayer: Boolean = false,
)

private const val PhoneQualifiers = "w360dp-h640dp"
private const val CompactLandscapeQualifiers = "w740dp-h320dp-land"
private const val FixturePlaylistId = "fixture"
private const val TransitionSettleMillis = 100L
private const val PositionTolerancePx = 0.5f

private const val RootTag = "main_navigation_fixture_root"
private const val NavigationHostTag = "main_navigation_fixture_host"
private const val HomeTag = "main_navigation_fixture_home"
private const val PlayerTag = "main_navigation_fixture_player"
private const val PlaylistTag = "main_navigation_fixture_playlist"
private const val BottomNavigationTag = "main_navigation_fixture_bottom_navigation"
private const val NavigationRailTag = "main_navigation_fixture_rail"
private const val MiniPlayerTag = "main_navigation_fixture_mini_player"
private const val NavigationRailTouchTag = "main_navigation_fixture_rail_touch"
private const val MiniPlayerTouchTag = "main_navigation_fixture_mini_player_touch"
private const val NavigationContentTouchTag = "main_navigation_fixture_content_touch"

private val HiddenChrome = ChromeExpectation()
private val PhoneHomeWithoutMiniChrome = ChromeExpectation(bottomNavigation = true)
private val PhoneHomeWithMiniChrome =
    ChromeExpectation(bottomNavigation = true, miniPlayer = true)
private val PlaylistWithMiniChrome = ChromeExpectation(miniPlayer = true)
private val RailHomeWithMiniChrome =
    ChromeExpectation(navigationRail = true, miniPlayer = true)
