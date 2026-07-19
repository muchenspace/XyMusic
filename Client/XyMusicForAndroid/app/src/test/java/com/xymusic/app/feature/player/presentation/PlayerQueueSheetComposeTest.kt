package com.xymusic.app.feature.player.presentation

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertWidthIsEqualTo
import androidx.compose.ui.test.junit4.ComposeContentTestRule
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.R
import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
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
class PlayerQueueSheetComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    @Config(qualifiers = "w740dp-h320dp-land")
    fun compactLandscapeUsesSingleControlRowAndCenteredLimitedContent() {
        var playbackModeCount = 0
        var clearCount = 0
        composeRule.setQueueContent(
            onCyclePlaybackMode = { playbackModeCount += 1 },
            onClear = { clearCount += 1 },
        )

        composeRule.onNodeWithTag(PlayerQueueTestTags.ContentPane).assertWidthIsEqualTo(720.dp)
        composeRule.onNodeWithTag(PlayerQueueTestTags.CompactHeader).assertIsDisplayed()
        composeRule.onNodeWithTag(PlayerQueueTestTags.List).assertIsDisplayed()

        val titleBounds =
            composeRule.onNodeWithTag(PlayerQueueTestTags.CompactTitle).fetchSemanticsNode().boundsInRoot
        val modeBounds =
            composeRule
                .onNodeWithTag(PlayerQueueTestTags.CompactPlaybackMode)
                .fetchSemanticsNode()
                .boundsInRoot
        val clearBounds =
            composeRule.onNodeWithTag(PlayerQueueTestTags.CompactClear).fetchSemanticsNode().boundsInRoot

        assertThat(titleBounds.right).isAtMost(modeBounds.left)
        assertThat(modeBounds.right).isAtMost(clearBounds.left)
        assertThat(abs(modeBounds.center.y - clearBounds.center.y)).isLessThan(2f)

        composeRule.onNodeWithTag(PlayerQueueTestTags.CompactPlaybackMode).performClick()
        composeRule.onNodeWithTag(PlayerQueueTestTags.CompactClear).performClick()
        assertThat(playbackModeCount).isEqualTo(1)
        assertThat(clearCount).isEqualTo(1)
    }

    @Test
    @Config(qualifiers = "w412dp-h915dp")
    fun portraitKeepsExistingTwoLevelQueueHeader() {
        composeRule.setQueueContent()

        composeRule.onNodeWithTag(PlayerQueueTestTags.CompactHeader).assertDoesNotExist()
        composeRule
            .onNodeWithText(resourceString(R.string.player_queue))
            .assertIsDisplayed()
        composeRule.onNodeWithText("Second Track").assertIsDisplayed()
    }

    private fun ComposeContentTestRule.setQueueContent(
        onCyclePlaybackMode: () -> Unit = {},
        onClear: () -> Unit = {},
    ) {
        setContent {
            XyMusicTheme(darkTheme = false) {
                QueueContent(
                    queue = listOf(queueItem("queue-1", "First Track"), queueItem("queue-2", "Second Track")),
                    currentQueueItemId = "queue-1",
                    shuffleEnabled = false,
                    repeatMode = RepeatMode.OFF,
                    onCyclePlaybackMode = onCyclePlaybackMode,
                    onSelect = {},
                    onRemove = {},
                    onMove = { _, _ -> },
                    onClear = onClear,
                )
            }
        }
    }

    private fun queueItem(queueItemId: String, title: String) = PlayerQueueItem(
        queueItemId = queueItemId,
        trackId = "track-$queueItemId",
        title = title,
        artistNames = listOf("Test Artist"),
        albumTitle = "Test Album",
        artworkUrl = null,
        artworkCacheKey = null,
        durationMs = 180_000,
    )

    private fun resourceString(resourceId: Int): String = RuntimeEnvironment.getApplication().getString(resourceId)
}
