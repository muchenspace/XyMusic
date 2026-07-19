package com.xymusic.app.app.trackactions

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.ComposeContentTestRule
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performScrollToIndex
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.R
import com.xymusic.app.feature.playlist.domain.model.PlaylistSummary
import com.xymusic.app.feature.playlist.domain.model.PlaylistVisibility
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMusicTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ComposeTestApplication::class)
class TrackActionsSheetComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    @Config(qualifiers = "w740dp-h320dp-land")
    fun compactLandscapeKeepsAllActionsReachableInOneScrollableList() {
        var favoriteCount = 0
        var downloadCount = 0
        var addedPlaylistId: String? = null
        val playlists = List(8) { index -> playlist(index) }
        composeRule.setTrackActionsSheet(
            uiState = TrackActionsUiState(selectedTrackId = "track-1", playlists = playlists),
            onToggleFavorite = { favoriteCount += 1 },
            onDownload = { downloadCount += 1 },
            onAddToPlaylist = { addedPlaylistId = it.id },
        )

        composeRule.waitForIdle()
        composeRule.onNodeWithTag(TrackActionsTestTags.CompactList).assertIsDisplayed()
        composeRule
            .onNodeWithText(resourceString(R.string.library_add_favorite))
            .assertIsDisplayed()
            .performClick()
        composeRule
            .onNodeWithText(resourceString(R.string.offline_download))
            .assertIsDisplayed()
            .performClick()
        assertThat(favoriteCount).isEqualTo(1)
        assertThat(downloadCount).isEqualTo(1)

        composeRule
            .onNodeWithText(resourceString(R.string.playlist_create_and_add))
            .performScrollTo()
            .assertIsDisplayed()
        val lastPlaylist = playlists.last()
        composeRule
            .onNodeWithTag(TrackActionsTestTags.CompactList)
            .performScrollToIndex(TRACK_ACTIONS_STATIC_ITEM_COUNT + playlists.lastIndex)
        val lastPlaylistNode =
            composeRule
                .onNodeWithTag(TrackActionsTestTags.playlist(lastPlaylist.id))
        val listBounds =
            composeRule
                .onNodeWithTag(TrackActionsTestTags.CompactList)
                .fetchSemanticsNode()
                .boundsInRoot
        val lastPlaylistBounds = lastPlaylistNode.fetchSemanticsNode().boundsInRoot
        assertThat(lastPlaylistBounds.top).isAtLeast(listBounds.top)
        assertThat(lastPlaylistBounds.bottom).isAtMost(listBounds.bottom)
        lastPlaylistNode.assertIsDisplayed().performClick()
        assertThat(addedPlaylistId).isEqualTo(lastPlaylist.id)
    }

    @Test
    @Config(qualifiers = "w412dp-h915dp")
    fun portraitKeepsExistingBottomSheetContent() {
        composeRule.setTrackActionsSheet(
            uiState = TrackActionsUiState(selectedTrackId = "track-1", playlists = listOf(playlist(0))),
        )

        composeRule.waitForIdle()
        composeRule.onNodeWithTag(TrackActionsTestTags.CompactList).assertDoesNotExist()
        composeRule.onNodeWithTag(TrackActionsTestTags.PortraitContent).assertIsDisplayed()
        composeRule
            .onNodeWithText(resourceString(R.string.library_add_favorite))
            .assertIsDisplayed()
    }

    private fun ComposeContentTestRule.setTrackActionsSheet(
        uiState: TrackActionsUiState,
        onToggleFavorite: () -> Unit = {},
        onDownload: () -> Unit = {},
        onAddToPlaylist: (PlaylistSummary) -> Unit = {},
    ) {
        setContent {
            XyMusicTheme(darkTheme = false) {
                TrackActionsSheet(
                    uiState = uiState,
                    onDismiss = {},
                    onToggleFavorite = onToggleFavorite,
                    onAddToPlaylist = onAddToPlaylist,
                    onCreatePlaylistAndAdd = { _, _, _ -> },
                    onDownload = onDownload,
                    onRemoveDownload = {},
                )
            }
        }
    }

    private fun playlist(index: Int) = PlaylistSummary(
        id = "playlist-$index",
        ownerUserId = "owner-1",
        name = "Playlist $index",
        description = null,
        visibility = PlaylistVisibility.PRIVATE,
        cover = null,
        trackCount = index + 1,
        version = 1,
        createdAtEpochMillis = 1,
        updatedAtEpochMillis = 1,
    )

    private fun resourceString(resourceId: Int): String = RuntimeEnvironment.getApplication().getString(resourceId)

    private companion object {
        const val TRACK_ACTIONS_STATIC_ITEM_COUNT = 5
    }
}
