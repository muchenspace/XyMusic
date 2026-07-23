package com.xymusic.app.feature.player.presentation

import android.view.View
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.mutableStateOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.test.assertHeightIsEqualTo
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsSelected
import androidx.compose.ui.test.assertWidthIsEqualTo
import androidx.compose.ui.test.hasAnyAncestor
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.ComposeContentTestRule
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performSemanticsAction
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.test.swipeLeft
import androidx.compose.ui.unit.dp
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.R
import com.xymusic.app.feature.player.domain.model.PlaybackState
import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
import com.xymusic.app.feature.player.domain.model.PlayerState
import com.xymusic.app.feature.player.domain.model.RepeatMode
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMusicTheme
import kotlin.math.abs
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ComposeTestApplication::class)
class PlayerScreenComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun miniBarWithArtworkShowsMetadataAndDispatchesCallbacks() {
        var openCount = 0
        var toggleCount = 0
        var nextCount = 0
        composeRule.setMiniBar(
            item =
            queueItem(
                artworkUrl = "https://media.example.test/cover.jpg?token=short-lived",
                artworkCacheKey = "artwork:track-1:v1",
            ),
            onOpenPlayer = { openCount += 1 },
            onTogglePlayback = { toggleCount += 1 },
            onNext = { nextCount += 1 },
        )

        composeRule
            .onNodeWithTag(
                PlayerTestTags.ArtworkImage,
                useUnmergedTree = true,
            ).fetchSemanticsNode()
        composeRule.onNodeWithTag(PlayerTestTags.MiniBar).assertHeightIsEqualTo(64.dp)
        composeRule.onNodeWithText("First Track").assertIsDisplayed()
        composeRule.onNodeWithText("First Artist").assertIsDisplayed()

        composeRule.onNodeWithTag(PlayerTestTags.TogglePlayback).performClick()
        assertThat(toggleCount).isEqualTo(1)
        assertThat(openCount).isEqualTo(0)

        composeRule.onNodeWithTag(PlayerTestTags.Next).performClick()
        assertThat(nextCount).isEqualTo(1)

        composeRule.onNodeWithTag(PlayerTestTags.OpenPlayer).performClick()
        composeRule
            .onNodeWithTag(PlayerTestTags.MiniBar)
            .performSemanticsAction(SemanticsActions.OnClick)
        assertThat(openCount).isEqualTo(2)
    }

    @Test
    fun miniBarWithoutArtworkShowsPlaceholderAndPlayAction() {
        composeRule.setMiniBar(item = queueItem())

        composeRule
            .onNodeWithTag(
                PlayerTestTags.ArtworkPlaceholder,
                useUnmergedTree = true,
            ).fetchSemanticsNode()
        composeRule
            .onNodeWithContentDescription(
                RuntimeEnvironment.getApplication().getString(R.string.player_play),
            ).assertIsDisplayed()
    }

    @Test
    fun miniBarPlayingStateKeepsLongMetadataVisibleAndShowsPauseAction() {
        val longTitle = "A very long track title that must stay inside the compact player bar"
        val longArtist = "A very long artist name that must be ellipsized without moving controls"
        composeRule.setMiniBar(
            item = queueItem(title = longTitle, artistNames = listOf(longArtist)),
            isPlaying = true,
        )

        composeRule.onNodeWithText(longTitle).assertIsDisplayed()
        composeRule.onNodeWithText(longArtist).assertIsDisplayed()
        composeRule
            .onNodeWithContentDescription(
                RuntimeEnvironment.getApplication().getString(R.string.player_pause),
            ).assertIsDisplayed()
        composeRule.onNodeWithTag(PlayerTestTags.TogglePlayback).assertIsDisplayed()
        composeRule.onNodeWithTag(PlayerTestTags.OpenPlayer).assertIsDisplayed()
    }

    @Test
    fun compactLandscapeMiniBarUsesShortChromeAndKeepsControlsUsable() {
        composeRule.setMiniBar(
            item = queueItem(),
            compact = true,
        )

        composeRule.onNodeWithTag(PlayerTestTags.MiniBar).assertHeightIsEqualTo(52.dp)
        composeRule
            .onNodeWithTag(PlayerTestTags.ArtworkPlaceholder, useUnmergedTree = true)
            .assertHeightIsEqualTo(40.dp)
            .assertWidthIsEqualTo(40.dp)
        composeRule
            .onNodeWithTag(PlayerTestTags.TogglePlayback)
            .assertHeightIsEqualTo(40.dp)
            .assertWidthIsEqualTo(40.dp)
        composeRule
            .onNodeWithTag(PlayerTestTags.Next)
            .assertHeightIsEqualTo(40.dp)
            .assertWidthIsEqualTo(40.dp)
    }

    @Test
    fun fullPlayerDispatchesPrimaryControlsAndBackNavigation() {
        val secondItem =
            queueItem(
                queueItemId = "queue-2",
                trackId = "track-2",
                title = "Second Track",
            )
        var backCount = 0
        var toggleCount = 0
        var previousCount = 0
        var nextCount = 0
        var playbackModeCount = 0
        composeRule.setPlayerScreen(
            uiState =
            PlayerUiState(
                player =
                playerState(
                    queue = listOf(queueItem(), secondItem),
                    positionMs = 60_000,
                    shuffleEnabled = true,
                    repeatMode = RepeatMode.ALL,
                ),
            ),
            onBack = { backCount += 1 },
            onTogglePlayback = { toggleCount += 1 },
            onPrevious = { previousCount += 1 },
            onNext = { nextCount += 1 },
            onCyclePlaybackMode = { playbackModeCount += 1 },
        )

        composeRule.onNodeWithTag(PlayerTestTags.TopBar).assertIsDisplayed()
        composeRule.onNodeWithText("First Track").assertIsDisplayed()
        composeRule.onNodeWithContentDescription(resourceString(R.string.player_previous)).performClick()
        composeRule.onNodeWithContentDescription(resourceString(R.string.player_play)).performClick()
        composeRule.onNodeWithContentDescription(resourceString(R.string.player_next)).performClick()
        composeRule.onNodeWithContentDescription(resourceString(R.string.player_playback_mode)).performClick()
        composeRule.swipePlayerToQueue()
        composeRule.onNodeWithContentDescription(resourceString(R.string.player_playback_mode)).performClick()
        composeRule.onNodeWithContentDescription(resourceString(R.string.common_back)).performClick()

        assertThat(toggleCount).isEqualTo(1)
        assertThat(previousCount).isEqualTo(1)
        assertThat(nextCount).isEqualTo(1)
        assertThat(playbackModeCount).isEqualTo(2)
        assertThat(backCount).isEqualTo(1)
    }

    @Test
    fun shuffledTimelineKeepsNextAvailableAtOriginalQueueEnd() {
        val secondItem =
            queueItem(
                queueItemId = "queue-2",
                trackId = "track-2",
                title = "Second Track",
            )
        var nextCount = 0
        composeRule.setPlayerScreen(
            uiState =
            PlayerUiState(
                player =
                playerState(
                    queue = listOf(queueItem(), secondItem),
                    shuffleEnabled = true,
                ).copy(currentQueueItemId = secondItem.queueItemId),
            ),
            onNext = { nextCount += 1 },
        )

        composeRule.onNodeWithContentDescription(resourceString(R.string.player_next)).performClick()

        assertThat(nextCount).isEqualTo(1)
    }

    @Test
    @Config(qualifiers = "w900dp-h420dp-land")
    fun landscapePlayerMatchesReferenceLayoutAndDispatchesBottomActions() {
        val secondItem =
            queueItem(
                queueItemId = "queue-2",
                trackId = "track-2",
                title = "Second Track",
            )
        var toggleCount = 0
        var previousCount = 0
        var nextCount = 0
        composeRule.setPlayerScreen(
            uiState =
            PlayerUiState(
                player = playerState(queue = listOf(queueItem(), secondItem), positionMs = 40_000),
                lyrics =
                listOf(
                    PlayerLyricLineUi(0, "Earlier lyric"),
                    PlayerLyricLineUi(20_000, "Previous lyric"),
                    PlayerLyricLineUi(40_000, "Centered landscape lyric"),
                    PlayerLyricLineUi(60_000, "Next lyric"),
                    PlayerLyricLineUi(80_000, "Later lyric"),
                ),
                synchronizedLyrics = true,
            ),
            onTogglePlayback = { toggleCount += 1 },
            onPrevious = { previousCount += 1 },
            onNext = { nextCount += 1 },
        )

        composeRule.onNodeWithTag(PlayerTestTags.TopBar).assertDoesNotExist()
        composeRule.onNodeWithContentDescription(resourceString(R.string.common_back)).assertDoesNotExist()
        composeRule.onNodeWithTag(PlayerTestTags.LandscapeArtworkPane).assertIsDisplayed()
        composeRule.onNodeWithTag(PlayerTestTags.LandscapeArtwork).assertIsDisplayed()
        composeRule.onNodeWithTag(PlayerTestTags.LandscapeLyricsPane).assertIsDisplayed()
        composeRule.onNodeWithTag(PlayerTestTags.LandscapeTrackHeader).assertDoesNotExist()
        composeRule
            .onNode(
                hasText("First Track", substring = true) and
                    hasAnyAncestor(hasTestTag(PlayerTestTags.LandscapeArtworkPane)),
            ).assertDoesNotExist()
        composeRule
            .onNode(
                hasText("First Artist", substring = true) and
                    hasAnyAncestor(hasTestTag(PlayerTestTags.LandscapeArtworkPane)),
            ).assertDoesNotExist()
        composeRule.onNodeWithText("First Album").assertDoesNotExist()
        composeRule.onNodeWithText("Centered landscape lyric").assertIsDisplayed().assertIsSelected()
        composeRule.onNodeWithTag(PlayerTestTags.LandscapePlaybackBar).assertDoesNotExist()
        composeRule.onNodeWithTag(PlayerTestTags.LandscapeTimeline).assertDoesNotExist()
        composeRule.onNodeWithTag(PlayerTestTags.LandscapeTime).assertDoesNotExist()
        composeRule.onNodeWithTag(PlayerTestTags.LandscapeTransport).assertIsDisplayed()
        composeRule.onNodeWithTag(PlayerTestTags.Favorite).assertDoesNotExist()

        val artworkPaneBounds =
            composeRule.onNodeWithTag(PlayerTestTags.LandscapeArtworkPane).fetchSemanticsNode().boundsInRoot
        val artworkBounds =
            composeRule.onNodeWithTag(PlayerTestTags.LandscapeArtwork).fetchSemanticsNode().boundsInRoot
        val lyricsPaneBounds =
            composeRule.onNodeWithTag(PlayerTestTags.LandscapeLyricsPane).fetchSemanticsNode().boundsInRoot
        val lyricBounds =
            composeRule.onNodeWithText("Centered landscape lyric").fetchSemanticsNode().boundsInRoot
        val transportBounds =
            composeRule.onNodeWithTag(PlayerTestTags.LandscapeTransport).fetchSemanticsNode().boundsInRoot
        val previousBounds = composeRule.onNodeWithTag(PlayerTestTags.Previous).fetchSemanticsNode().boundsInRoot
        val playBounds =
            composeRule.onNodeWithTag(PlayerTestTags.TogglePlayback).fetchSemanticsNode().boundsInRoot
        val nextBounds = composeRule.onNodeWithTag(PlayerTestTags.Next).fetchSemanticsNode().boundsInRoot

        assertThat(artworkBounds.center.x).isLessThan(lyricsPaneBounds.center.x)
        assertThat(abs(artworkBounds.left - artworkPaneBounds.left)).isLessThan(2f)
        assertThat(abs(transportBounds.width - artworkBounds.width)).isLessThan(2f)
        assertThat(artworkBounds.bottom).isLessThan(transportBounds.top)
        assertThat(artworkBounds.height).isGreaterThan(transportBounds.height * 5f)
        assertThat(abs(lyricBounds.center.y - lyricsPaneBounds.center.y)).isLessThan(2f)
        assertThat(previousBounds.center.x).isLessThan(playBounds.center.x)
        assertThat(playBounds.center.x).isLessThan(nextBounds.center.x)
        assertThat(
            abs(
                (playBounds.center.x - previousBounds.center.x) -
                    (nextBounds.center.x - playBounds.center.x),
            ),
        ).isLessThan(2f)
        assertThat(playBounds.width).isGreaterThan(previousBounds.width)
        assertThat(playBounds.width).isGreaterThan(nextBounds.width)
        assertThat(previousBounds.left).isGreaterThan(artworkBounds.left)
        assertThat(nextBounds.right).isLessThan(artworkBounds.right)

        composeRule.onNodeWithTag(PlayerTestTags.Previous).performClick()
        composeRule.onNodeWithTag(PlayerTestTags.TogglePlayback).performClick()
        composeRule.onNodeWithTag(PlayerTestTags.Next).performClick()
        composeRule.onNodeWithTag(PlayerTestTags.ContentPager).assertIsDisplayed()
        assertThat(previousCount).isEqualTo(1)
        assertThat(toggleCount).isEqualTo(1)
        assertThat(nextCount).isEqualTo(1)
    }

    @Test
    @Config(qualifiers = "w900dp-h420dp-land")
    fun landscapePlayerSwipesOnceToQueue() {
        val secondItem =
            queueItem(
                queueItemId = "queue-2",
                trackId = "track-2",
                title = "Second Track",
            )
        var selectedQueueItem: String? = null
        composeRule.setPlayerScreen(
            uiState = PlayerUiState(player = playerState(queue = listOf(secondItem))),
            onSelectQueueItem = { selectedQueueItem = it },
        )

        composeRule.onNodeWithTag(PlayerTestTags.TopBar).assertDoesNotExist()
        composeRule.onNodeWithTag(PlayerTestTags.ContentPager).performTouchInput { swipeLeft() }
        composeRule.waitForIdle()

        composeRule.onNodeWithTag(PlayerTestTags.QueueContent).assertIsDisplayed()
        composeRule
            .onNode(
                hasText("Second Track") and
                    hasAnyAncestor(hasTestTag(PlayerTestTags.QueueContent)),
            ).assertIsDisplayed()
            .performClick()
        assertThat(selectedQueueItem).isEqualTo("queue-2")
    }

    @Test
    @Config(qualifiers = "w740dp-h320dp-land")
    fun compactLandscapeKeepsReferenceRegionsVisible() {
        composeRule.setPlayerScreen(
            uiState =
            PlayerUiState(
                player = playerState(positionMs = 80_000),
                lyrics =
                listOf(
                    PlayerLyricLineUi(0, "Compact first lyric"),
                    PlayerLyricLineUi(40_000, "Compact middle lyric"),
                    PlayerLyricLineUi(80_000, "Compact centered last lyric"),
                ),
                synchronizedLyrics = true,
            ),
        )

        composeRule.onNodeWithTag(PlayerTestTags.LandscapeArtwork).assertIsDisplayed()
        composeRule.onNodeWithTag(PlayerTestTags.LandscapeTrackHeader).assertDoesNotExist()
        composeRule.onNodeWithText("Compact centered last lyric").assertIsDisplayed()
        composeRule.onNodeWithTag(PlayerTestTags.LandscapeTransport).assertIsDisplayed()
        composeRule.onNodeWithTag(PlayerTestTags.Favorite).assertDoesNotExist()
        val artworkPaneBounds =
            composeRule.onNodeWithTag(PlayerTestTags.LandscapeArtworkPane).fetchSemanticsNode().boundsInRoot
        val lyricsPaneBounds =
            composeRule.onNodeWithTag(PlayerTestTags.LandscapeLyricsPane).fetchSemanticsNode().boundsInRoot
        val lyricBounds =
            composeRule.onNodeWithText("Compact centered last lyric").fetchSemanticsNode().boundsInRoot
        val artworkBounds =
            composeRule.onNodeWithTag(PlayerTestTags.LandscapeArtwork).fetchSemanticsNode().boundsInRoot
        val transportBounds =
            composeRule.onNodeWithTag(PlayerTestTags.LandscapeTransport).fetchSemanticsNode().boundsInRoot
        val previousBounds = composeRule.onNodeWithTag(PlayerTestTags.Previous).fetchSemanticsNode().boundsInRoot
        val playBounds =
            composeRule.onNodeWithTag(PlayerTestTags.TogglePlayback).fetchSemanticsNode().boundsInRoot
        val nextBounds = composeRule.onNodeWithTag(PlayerTestTags.Next).fetchSemanticsNode().boundsInRoot
        assertThat(abs(artworkBounds.left - artworkPaneBounds.left)).isLessThan(2f)
        assertThat(abs(transportBounds.width - artworkBounds.width)).isLessThan(2f)
        assertThat(artworkBounds.height).isGreaterThan(transportBounds.height * 4f)
        assertThat(
            abs(
                (playBounds.center.x - previousBounds.center.x) -
                    (nextBounds.center.x - playBounds.center.x),
            ),
        ).isLessThan(2f)
        assertThat(previousBounds.left).isGreaterThan(artworkBounds.left)
        assertThat(nextBounds.right).isLessThan(artworkBounds.right)
        assertThat(abs(lyricBounds.center.y - lyricsPaneBounds.center.y)).isLessThan(2f)
    }

    @Test
    @Config(qualifiers = "w900dp-h420dp-land")
    fun landscapePlayerKeepsScreenOnOnlyWhileComposed() {
        val showPlayer = mutableStateOf(true)
        lateinit var rootView: View
        composeRule.setContent {
            rootView = LocalView.current
            XyMusicTheme(darkTheme = false) {
                if (showPlayer.value) {
                    PlayerScreen(
                        uiState = PlayerUiState(player = playerState()),
                        onBack = {},
                        onTogglePlayback = {},
                        onSeek = {},
                        onPrevious = {},
                        onNext = {},
                        onCyclePlaybackMode = {},
                        onSelectQueueItem = {},
                        onRemoveQueueItem = {},
                        onMoveQueueItem = { _, _ -> },
                        onClearQueue = {},
                        onPlaybackSpeedChange = {},
                        onSleepTimerChange = {},
                        onToggleFavorite = {},
                        onAddToPlaylist = {},
                    )
                }
            }
        }

        composeRule.waitForIdle()
        assertThat(rootView.keepScreenOn).isTrue()

        composeRule.runOnIdle { showPlayer.value = false }
        composeRule.waitForIdle()
        assertThat(rootView.keepScreenOn).isFalse()
    }

    @Test
    fun emptyPlayerStillExposesBackNavigation() {
        var backCount = 0
        composeRule.setPlayerScreen(
            uiState = PlayerUiState(),
            onBack = { backCount += 1 },
        )

        composeRule.onNodeWithContentDescription(resourceString(R.string.common_back)).performClick()

        assertThat(backCount).isEqualTo(1)
    }

    @Test
    fun lyricsTabMarksTheCurrentLineAsSelected() {
        composeRule.setPlayerScreen(
            uiState =
            PlayerUiState(
                player = playerState(positionMs = 10_000),
                lyrics =
                listOf(
                    PlayerLyricLineUi(0, "First lyric"),
                    PlayerLyricLineUi(10_000, "Current lyric"),
                    PlayerLyricLineUi(20_000, "Last lyric"),
                ),
                synchronizedLyrics = true,
            ),
        )

        composeRule.swipePlayerToLyrics()

        composeRule.onNodeWithText("Current lyric").assertIsDisplayed().assertIsSelected()
    }

    @Test
    fun swipingLeftRevealsLyrics() {
        composeRule.setPlayerScreen(
            uiState =
            PlayerUiState(
                player = playerState(),
                lyrics = listOf(PlayerLyricLineUi(0, "Swipe lyric")),
            ),
        )

        composeRule.swipePlayerToLyrics()

        composeRule.onNodeWithText("Swipe lyric").assertIsDisplayed()
    }

    @Test
    fun plainLyricsRemainReadableWithoutAnActiveLine() {
        composeRule.setPlayerScreen(
            uiState =
            PlayerUiState(
                player = playerState(),
                lyrics =
                listOf(
                    PlayerLyricLineUi(null, "Plain first line"),
                    PlayerLyricLineUi(null, "Plain second line"),
                ),
                synchronizedLyrics = false,
            ),
        )

        composeRule.swipePlayerToLyrics()

        composeRule.onNodeWithText("Plain first line").assertIsDisplayed()
        composeRule.onNodeWithText("Plain second line").assertIsDisplayed()
    }

    @Test
    fun clickingTimedLyricSeeksToItsTimestamp() {
        var seekPosition: Long? = null
        composeRule.setPlayerScreen(
            uiState =
            PlayerUiState(
                player = playerState(),
                lyrics =
                listOf(
                    PlayerLyricLineUi(0, "First lyric"),
                    PlayerLyricLineUi(12_345, "Jump lyric"),
                ),
                synchronizedLyrics = true,
            ),
            onSeek = { seekPosition = it },
        )

        composeRule.swipePlayerToLyrics()
        composeRule.onNodeWithText("Jump lyric").performClick()

        assertThat(seekPosition).isEqualTo(12_345)
    }

    @Test
    fun queueSheetSelectsItems() {
        val secondItem =
            queueItem(
                queueItemId = "queue-2",
                trackId = "track-2",
                title = "Second Track",
            )
        var selectedQueueItem: String? = null
        composeRule.setPlayerScreen(
            uiState = PlayerUiState(player = playerState(queue = listOf(queueItem(), secondItem))),
            onSelectQueueItem = { selectedQueueItem = it },
        )

        composeRule.swipePlayerToQueue()
        composeRule.onNodeWithText("Second Track").assertIsDisplayed().performClick()
        assertThat(selectedQueueItem).isEqualTo("queue-2")
    }

    @Test
    fun queueSheetRequiresConfirmationBeforeClearing() {
        var clearCount = 0
        composeRule.setPlayerScreen(
            uiState = PlayerUiState(player = playerState()),
            onClearQueue = { clearCount += 1 },
        )

        composeRule.swipePlayerToQueue()
        val clearLabel = resourceString(R.string.player_clear_queue)
        composeRule.onNodeWithText(clearLabel).performClick()
        composeRule.onNodeWithText(resourceString(R.string.player_clear_queue_title)).assertIsDisplayed()
        assertThat(clearCount).isEqualTo(0)
        composeRule.onAllNodesWithText(clearLabel)[1].performClick()

        assertThat(clearCount).isEqualTo(1)
    }

    private fun ComposeContentTestRule.setMiniBar(
        item: PlayerQueueItem,
        isPlaying: Boolean = false,
        onOpenPlayer: () -> Unit = {},
        onTogglePlayback: () -> Unit = {},
        onNext: () -> Unit = {},
        compact: Boolean = false,
    ) {
        setContent {
            XyMusicTheme(darkTheme = false) {
                Box(modifier = Modifier.width(320.dp)) {
                    PlayerMiniBar(
                        uiState =
                        PlayerUiState(
                            player =
                            PlayerState(
                                queue = listOf(item),
                                currentQueueItemId = item.queueItemId,
                                isPlaying = isPlaying,
                                positionMs = 60_000,
                                durationMs = item.durationMs,
                            ),
                        ),
                        onOpenPlayer = onOpenPlayer,
                        onTogglePlayback = onTogglePlayback,
                        onNext = onNext,
                        compact = compact,
                    )
                }
            }
        }
    }

    private fun ComposeContentTestRule.swipePlayerToLyrics() {
        onNodeWithTag(PlayerTestTags.ContentPager).performTouchInput { swipeLeft() }
        waitForIdle()
    }

    private fun ComposeContentTestRule.swipePlayerToQueue() {
        repeat(2) {
            onNodeWithTag(PlayerTestTags.ContentPager).performTouchInput { swipeLeft() }
            waitForIdle()
        }
    }

    private fun ComposeContentTestRule.setPlayerScreen(
        uiState: PlayerUiState,
        onBack: () -> Unit = {},
        onTogglePlayback: () -> Unit = {},
        onSeek: (Long) -> Unit = {},
        onPrevious: () -> Unit = {},
        onNext: () -> Unit = {},
        onCyclePlaybackMode: () -> Unit = {},
        onSelectQueueItem: (String) -> Unit = {},
        onClearQueue: () -> Unit = {},
        onToggleFavorite: () -> Unit = {},
        onAddToPlaylist: () -> Unit = {},
    ) {
        setContent {
            XyMusicTheme(darkTheme = false) {
                PlayerScreen(
                    uiState = uiState,
                    onBack = onBack,
                    onTogglePlayback = onTogglePlayback,
                    onSeek = onSeek,
                    onPrevious = onPrevious,
                    onNext = onNext,
                    onCyclePlaybackMode = onCyclePlaybackMode,
                    onSelectQueueItem = onSelectQueueItem,
                    onRemoveQueueItem = {},
                    onMoveQueueItem = { _, _ -> },
                    onClearQueue = onClearQueue,
                    onPlaybackSpeedChange = {},
                    onSleepTimerChange = {},
                    onToggleFavorite = onToggleFavorite,
                    onAddToPlaylist = onAddToPlaylist,
                )
            }
        }
    }

    private fun playerState(
        queue: List<PlayerQueueItem> = listOf(queueItem()),
        positionMs: Long = 0,
        shuffleEnabled: Boolean = false,
        repeatMode: RepeatMode = RepeatMode.OFF,
    ) = PlayerState(
        playbackState = PlaybackState.READY,
        queue = queue,
        currentQueueItemId = queue.first().queueItemId,
        positionMs = positionMs,
        durationMs = queue.first().durationMs,
        shuffleEnabled = shuffleEnabled,
        repeatMode = repeatMode,
    )

    private fun resourceString(resourceId: Int): String = RuntimeEnvironment.getApplication().getString(resourceId)

    private fun queueItem(
        queueItemId: String = "queue-1",
        trackId: String = "track-1",
        title: String = "First Track",
        artistNames: List<String> = listOf("First Artist"),
        artworkUrl: String? = null,
        artworkCacheKey: String? = null,
    ) = PlayerQueueItem(
        queueItemId = queueItemId,
        trackId = trackId,
        title = title,
        artistNames = artistNames,
        albumTitle = "First Album",
        artworkUrl = artworkUrl,
        artworkCacheKey = artworkCacheKey,
        durationMs = 240_000,
    )
}
