package com.xymusic.app.app.mine

import android.content.Context
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.R
import com.xymusic.app.feature.library.presentation.LibraryTab
import com.xymusic.app.feature.library.presentation.LibraryUiState
import com.xymusic.app.feature.playlist.domain.model.PlaylistSummary
import com.xymusic.app.feature.playlist.domain.model.PlaylistVisibility
import com.xymusic.app.feature.settings.domain.model.UserProfile
import com.xymusic.app.feature.settings.domain.model.UserRole
import com.xymusic.app.feature.settings.domain.model.UserStatus
import com.xymusic.app.feature.settings.presentation.SettingsUiState
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMusicTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ComposeTestApplication::class)
class MineScreenComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    private val context: Context
        get() = ApplicationProvider.getApplicationContext()

    @Test
    fun profileAndPlaylistGridUseProvidedState() {
        val playlist = playlist()

        composeRule.setMineContent(
            settingsUiState = SettingsUiState(profile = profile()),
            libraryUiState = LibraryUiState(playlists = listOf(playlist)),
        )

        composeRule.onNodeWithText("Lin Chen").assertIsDisplayed()
        composeRule.onNodeWithText("Listening across devices").assertIsDisplayed()
        composeRule.onNodeWithText("@linchen").assertIsDisplayed()
        composeRule
            .onNodeWithTag(MineTestTags.Root)
            .performScrollToNode(hasTestTag(MineTestTags.playlist(playlist.id)))
        composeRule
            .onNodeWithTag(MineTestTags.playlist(playlist.id))
            .assertIsDisplayed()
        composeRule.onNodeWithText("Night Drive").assertIsDisplayed()
        composeRule
            .onNodeWithText(
                context.getString(R.string.playlist_track_count, playlist.trackCount),
            ).assertIsDisplayed()
    }

    @Test
    fun emptyPlaylistStateKeepsCreateActionAvailable() {
        var createRequests = 0

        composeRule.setMineContent(
            settingsUiState = SettingsUiState(profile = profile()),
            libraryUiState = LibraryUiState(),
            onCreatePlaylist = { createRequests += 1 },
        )

        val emptyTitle = context.getString(R.string.playlist_empty_title)
        val emptyMessage = context.getString(R.string.playlist_empty_message)
        val createLabel = context.getString(R.string.playlist_create)
        composeRule.onNodeWithTag(MineTestTags.Root).performScrollToNode(hasText(emptyTitle))
        composeRule.onNodeWithText(emptyTitle).assertIsDisplayed()
        composeRule.onNodeWithTag(MineTestTags.Root).performScrollToNode(hasText(emptyMessage))
        composeRule.onNodeWithText(emptyMessage).assertIsDisplayed()
        composeRule.onNodeWithTag(MineTestTags.Root).performScrollToNode(hasText(createLabel))
        composeRule.onNodeWithText(createLabel).performClick()

        assertThat(createRequests).isEqualTo(1)
    }

    @Test
    fun settingsLibraryCreateAndPlaylistCallbacksAreDispatched() {
        val playlist = playlist()
        var settingsRequests = 0
        val libraryRequests = mutableListOf<LibraryTab>()
        var createRequests = 0
        var selectedPlaylist: String? = null

        composeRule.setMineContent(
            settingsUiState = SettingsUiState(profile = profile()),
            libraryUiState = LibraryUiState(playlists = listOf(playlist)),
            onPlaylistClick = { selectedPlaylist = it },
            onOpenLibrary = libraryRequests::add,
            onOpenSettings = { settingsRequests += 1 },
            onCreatePlaylist = { createRequests += 1 },
        )

        composeRule.onNodeWithTag(MineTestTags.Settings).performClick()
        composeRule.onNodeWithTag(MineTestTags.Favorites).performClick()
        composeRule.onNodeWithTag(MineTestTags.Playlists).performClick()
        composeRule.onNodeWithTag(MineTestTags.History).performClick()
        composeRule
            .onNodeWithTag(MineTestTags.Root)
            .performScrollToNode(hasTestTag(MineTestTags.CreatePlaylist))
        composeRule
            .onNodeWithTag(MineTestTags.CreatePlaylist)
            .performClick()
        composeRule
            .onNodeWithTag(MineTestTags.Root)
            .performScrollToNode(hasTestTag(MineTestTags.playlist(playlist.id)))
        composeRule
            .onNodeWithTag(MineTestTags.playlist(playlist.id))
            .performClick()

        assertThat(settingsRequests).isEqualTo(1)
        assertThat(libraryRequests)
            .containsExactly(
                LibraryTab.Favorites,
                LibraryTab.Playlists,
                LibraryTab.History,
            ).inOrder()
        assertThat(createRequests).isEqualTo(1)
        assertThat(selectedPlaylist).isEqualTo(playlist.id)
    }

    @Test
    @Config(qualifiers = "w900dp-h420dp-land")
    fun landscapeMineShowsAccountAndPlaylistsSideBySide() {
        val playlist = playlist()
        composeRule.setMineContent(
            settingsUiState = SettingsUiState(profile = profile()),
            libraryUiState = LibraryUiState(playlists = listOf(playlist)),
        )

        val accountBounds =
            composeRule.onNodeWithTag(
                MineTestTags.LandscapeAccountPane,
            ).assertIsDisplayed().fetchSemanticsNode().boundsInRoot
        val playlistBounds =
            composeRule
                .onNodeWithTag(MineTestTags.LandscapePlaylistPane)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot

        assertThat(accountBounds.right).isLessThan(playlistBounds.left)
        composeRule.onNodeWithText("Lin Chen").assertIsDisplayed()
        composeRule.onNodeWithTag(MineTestTags.Favorites).assertIsDisplayed()
        composeRule.onNodeWithTag(MineTestTags.playlist(playlist.id)).assertIsDisplayed()
    }

    private fun profile() = UserProfile(
        id = "user-1",
        username = "linchen",
        displayName = "Lin Chen",
        bio = "Listening across devices",
        avatar = null,
        role = UserRole.USER,
        status = UserStatus.ACTIVE,
        version = 1L,
        createdAtEpochMillis = 1_000L,
        updatedAtEpochMillis = 1_000L,
    )

    private fun playlist() = PlaylistSummary(
        id = "playlist-1",
        ownerUserId = "user-1",
        name = "Night Drive",
        description = "Late-night favorites",
        visibility = PlaylistVisibility.PRIVATE,
        cover = null,
        trackCount = 12,
        version = 1L,
        createdAtEpochMillis = 1_000L,
        updatedAtEpochMillis = 1_000L,
    )

    private fun androidx.compose.ui.test.junit4.ComposeContentTestRule.setMineContent(
        settingsUiState: SettingsUiState,
        libraryUiState: LibraryUiState,
        onPlaylistClick: (String) -> Unit = {},
        onOpenLibrary: (LibraryTab) -> Unit = {},
        onOpenSettings: () -> Unit = {},
        onCreatePlaylist: () -> Unit = {},
    ) {
        setContent {
            XyMusicTheme(darkTheme = false) {
                MineContent(
                    settingsUiState = settingsUiState,
                    libraryUiState = libraryUiState,
                    onPlaylistClick = onPlaylistClick,
                    onOpenLibrary = onOpenLibrary,
                    onOpenSettings = onOpenSettings,
                    onCreatePlaylist = onCreatePlaylist,
                )
            }
        }
    }
}
