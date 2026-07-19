package com.xymusic.app.feature.playlist.presentation

import android.content.Context
import androidx.compose.material3.SnackbarHostState
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasScrollToIndexAction
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.isFocused
import androidx.compose.ui.test.junit4.ComposeContentTestRule
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performScrollToIndex
import androidx.compose.ui.test.performTextReplacement
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.R
import com.xymusic.app.core.ui.media.CatalogAlbumLinkUi
import com.xymusic.app.core.ui.media.CatalogArtistLinkUi
import com.xymusic.app.core.ui.media.CatalogTrackUi
import com.xymusic.app.feature.playlist.domain.model.PlaylistVisibility
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMusicTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ComposeTestApplication::class)
class PlaylistScreenComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    private val context: Context
        get() = ApplicationProvider.getApplicationContext()

    @Test
    fun populatedPlaylistDisplaysMetadataAndDispatchesPlayback() {
        var playedEntryId: String? = "unset"
        composeRule.setPlaylistContent(
            uiState = PlaylistUiState(detail = detail()),
            onPlay = { playedEntryId = it },
        )

        composeRule.onNodeWithText("Night Drive").assertIsDisplayed()
        composeRule.onNodeWithText("Late-night favorites").assertIsDisplayed()
        composeRule
            .onNodeWithText(context.getString(R.string.playlist_play_all))
            .performScrollTo()
            .performClick()
        assertThat(playedEntryId).isNull()

        composeRule.onNode(hasScrollToIndexAction()).performScrollToIndex(1)
        composeRule.onNodeWithText("First Track").assertIsDisplayed().performClick()
        assertThat(playedEntryId).isEqualTo("entry-1")
    }

    @Test
    fun editMenuPrefillsFieldsAndDispatchesUpdatedValues() {
        var updatedName: String? = null
        var updatedDescription: String? = null
        var updatedVisibility: PlaylistVisibility? = null
        composeRule.setPlaylistContent(
            uiState = PlaylistUiState(detail = detail()),
            onUpdate = { name, description, visibility ->
                updatedName = name
                updatedDescription = description
                updatedVisibility = visibility
            },
        )

        composeRule.onNodeWithContentDescription(context.getString(R.string.common_more_actions)).performClick()
        composeRule
            .onNode(
                hasText(context.getString(R.string.playlist_edit)) and isFocused(),
            ).performClick()
        composeRule
            .onNode(hasSetTextAction() and hasText("Night Drive"))
            .performTextReplacement("Morning Focus")
        composeRule.onNodeWithText(context.getString(R.string.playlist_visibility_public)).performClick()
        composeRule.onNodeWithText(context.getString(R.string.common_confirm)).performClick()

        assertThat(updatedName).isEqualTo("Morning Focus")
        assertThat(updatedDescription).isEqualTo("Late-night favorites")
        assertThat(updatedVisibility).isEqualTo(PlaylistVisibility.PUBLIC)
    }

    @Test
    fun deleteMenuRequiresConfirmation() {
        var deleteCount = 0
        composeRule.setPlaylistContent(
            uiState = PlaylistUiState(detail = detail()),
            onDelete = { deleteCount += 1 },
        )

        composeRule.onNodeWithContentDescription(context.getString(R.string.common_more_actions)).performClick()
        composeRule.onNodeWithText(context.getString(R.string.playlist_delete)).performClick()
        composeRule.onNodeWithText(context.getString(R.string.playlist_delete_title)).assertIsDisplayed()
        assertThat(deleteCount).isEqualTo(0)
        composeRule.onNodeWithText(context.getString(R.string.playlist_delete)).performClick()

        assertThat(deleteCount).isEqualTo(1)
    }

    @Test
    fun loadingStateKeepsBackAndRefreshActionsAvailable() {
        var backCount = 0
        var refreshCount = 0
        composeRule.setPlaylistContent(
            uiState = PlaylistUiState(),
            onBack = { backCount += 1 },
            onRefresh = { refreshCount += 1 },
        )

        composeRule.onNodeWithContentDescription(context.getString(R.string.common_back)).performClick()
        composeRule.onNodeWithContentDescription(context.getString(R.string.catalog_refresh)).performClick()

        assertThat(backCount).isEqualTo(1)
        assertThat(refreshCount).isEqualTo(1)
    }

    @Test
    @Config(qualifiers = "w740dp-h320dp-land")
    fun compactLandscapeShowsMetadataBesideFirstTrack() {
        composeRule.setPlaylistContent(uiState = PlaylistUiState(detail = detail()))

        composeRule.waitForIdle()
        val header =
            composeRule
                .onNodeWithTag(PlaylistDetailTestTags.LandscapeHeader)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot
        val tracks =
            composeRule
                .onNodeWithTag(PlaylistDetailTestTags.LandscapeTracks)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot
        val firstTrack =
            composeRule
                .onNodeWithTag(PlaylistDetailTestTags.track("entry-1"))
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot

        assertThat(header.right).isAtMost(tracks.left + 1f)
        assertThat(firstTrack.top).isAtLeast(tracks.top)
        assertThat(firstTrack.bottom).isAtMost(tracks.bottom + 1f)
    }

    @Test
    @Config(qualifiers = "w740dp-h320dp-land")
    fun compactLandscapeEditorKeepsScrollableFormAndActionsReachable() {
        var updatedVisibility: PlaylistVisibility? = null
        composeRule.setPlaylistContent(
            uiState = PlaylistUiState(detail = detail()),
            onUpdate = { _, _, visibility -> updatedVisibility = visibility },
        )

        composeRule.onNodeWithText(context.getString(R.string.playlist_edit)).performClick()
        composeRule
            .onNodeWithText(context.getString(R.string.playlist_visibility_public))
            .performScrollTo()
            .performClick()
        composeRule
            .onNodeWithText(context.getString(R.string.common_confirm))
            .performScrollTo()
            .assertIsDisplayed()
            .performClick()

        assertThat(updatedVisibility).isEqualTo(PlaylistVisibility.PUBLIC)
    }

    private fun ComposeContentTestRule.setPlaylistContent(
        uiState: PlaylistUiState,
        onBack: () -> Unit = {},
        onRefresh: () -> Unit = {},
        onPlay: (String?) -> Unit = {},
        onUpdate: (String, String?, PlaylistVisibility) -> Unit = { _, _, _ -> },
        onDelete: () -> Unit = {},
    ) {
        setContent {
            XyMusicTheme(darkTheme = false) {
                PlaylistScreen(
                    uiState = uiState,
                    snackbarHostState = SnackbarHostState(),
                    onBack = onBack,
                    onRefresh = onRefresh,
                    onLoadMore = {},
                    onPlay = onPlay,
                    onUpdate = onUpdate,
                    onDelete = onDelete,
                    onRemove = {},
                    onReorder = {},
                    onTrackMore = {},
                )
            }
        }
    }

    private fun detail() = PlaylistDetailUi(
        id = "playlist-1",
        name = "Night Drive",
        description = "Late-night favorites",
        visibility = PlaylistVisibility.PRIVATE,
        trackCount = 1,
        version = 3,
        entries =
        listOf(
            PlaylistEntryUi(
                entryId = "entry-1",
                position = 0,
                track =
                CatalogTrackUi(
                    id = "track-1",
                    title = "First Track",
                    artists = listOf(CatalogArtistLinkUi("artist-1", "First Artist")),
                    album = CatalogAlbumLinkUi("album-1", "First Album"),
                    artwork = null,
                    durationMs = 180_000,
                    discNumber = 1,
                    trackNumber = 1,
                ),
            ),
        ),
    )
}
