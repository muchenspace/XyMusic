package com.xymusic.app.app.home

import android.content.Context
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertHeightIsEqualTo
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertWidthIsEqualTo
import androidx.compose.ui.test.junit4.ComposeContentTestRule
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToIndex
import androidx.compose.ui.unit.dp
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.R
import com.xymusic.app.core.ui.media.CatalogAlbumLinkUi
import com.xymusic.app.core.ui.media.CatalogAlbumUi
import com.xymusic.app.core.ui.media.CatalogArtistLinkUi
import com.xymusic.app.core.ui.media.CatalogTrackUi
import com.xymusic.app.feature.catalog.presentation.CatalogRandomUiState
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMusicTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ComposeTestApplication::class)
class HomeScreenComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    private val context: Context
        get() = ApplicationProvider.getApplicationContext()

    @Test
    fun searchCapsuleDispatchesClick() {
        var searchClicked = false
        composeRule.setHomeContent(
            onSearchClick = { searchClicked = true },
        )

        composeRule
            .onNodeWithTag(HomeTestTags.Search)
            .assertIsDisplayed()
            .performClick()

        composeRule.runOnIdle {
            assertThat(searchClicked).isTrue()
        }
    }

    @Test
    fun profileButtonDispatchesClick() {
        var profileClicked = false
        composeRule.setHomeContent(onProfileClick = { profileClicked = true })

        composeRule
            .onNodeWithTag(HomeTestTags.Profile)
            .assertIsDisplayed()
            .performClick()

        composeRule.runOnIdle {
            assertThat(profileClicked).isTrue()
        }
    }

    @Test
    fun realProfileAvatarKeepsTheExistingButtonAndImageSize() {
        composeRule.setHomeContent(
            profileUiState =
            HomeProfileUiState(
                avatarUrl = "https://media.example/avatar.jpg",
                avatarCacheKey = "avatar:user-1:v2",
            ),
        )

        composeRule
            .onNodeWithTag(HomeTestTags.Profile)
            .assertWidthIsEqualTo(44.dp)
            .assertHeightIsEqualTo(44.dp)
        composeRule
            .onNodeWithTag(HomeTestTags.ProfileAvatar, useUnmergedTree = true)
            .assertWidthIsEqualTo(29.dp)
            .assertHeightIsEqualTo(29.dp)
        composeRule
            .onNodeWithTag(HomeTestTags.ProfileImage, useUnmergedTree = true)
            .fetchSemanticsNode()
    }

    @Test
    fun missingProfileAvatarKeepsTheDefaultPlaceholder() {
        composeRule.setHomeContent()

        composeRule
            .onNodeWithTag(HomeTestTags.ProfilePlaceholder, useUnmergedTree = true)
            .fetchSemanticsNode()
    }

    @Test
    fun featuredAlbumOpensItsDetailFromThePrimaryClickTarget() {
        var openedAlbum: String? = null
        composeRule.setHomeContent(
            onAlbumClick = { openedAlbum = it },
        )

        composeRule
            .onNodeWithTag(HomeTestTags.featuredAlbum("album-1"))
            .assertIsDisplayed()
            .performClick()
        composeRule.runOnIdle {
            assertThat(openedAlbum).isEqualTo("album-1")
        }
    }

    @Test
    fun featuredFailureRetriesWithoutHidingRecommendedTracks() {
        var retryCount = 0
        composeRule.setHomeContent(
            featuredAlbums = emptyList(),
            featuredFailed = true,
            onRetryFeatured = { retryCount += 1 },
        )

        composeRule
            .onNodeWithText(context.getString(R.string.common_retry))
            .assertIsDisplayed()
            .performClick()
        composeRule.onNodeWithTag(HomeTestTags.DiscoverList).performScrollToIndex(3)
        composeRule.onNodeWithText("Track One").fetchSemanticsNode()
        composeRule.runOnIdle {
            assertThat(retryCount).isEqualTo(1)
        }
    }

    @Test
    fun recommendedFailureRetriesWithoutHidingFeaturedAlbums() {
        var retryCount = 0
        composeRule.setHomeContent(
            recommendedTracks = emptyList(),
            recommendedFailed = true,
            onRetryRecommended = { retryCount += 1 },
        )

        composeRule.onNodeWithTag(HomeTestTags.featuredAlbum("album-1")).fetchSemanticsNode()
        composeRule.onNodeWithTag(HomeTestTags.DiscoverList).performScrollToIndex(4)
        composeRule
            .onNodeWithText(context.getString(R.string.common_retry))
            .assertIsDisplayed()
            .performClick()
        composeRule.runOnIdle {
            assertThat(retryCount).isEqualTo(1)
        }
    }

    @Test
    fun emptyCatalogKeepsSearchReachable() {
        val emptyTitle = context.getString(R.string.home_empty_title)
        val emptyMessage = context.getString(R.string.home_empty_message)
        composeRule.setHomeContent(
            tracks = emptyList(),
            featuredAlbums = emptyList(),
            recommendedTracks = emptyList(),
        )

        composeRule.onNodeWithTag(HomeTestTags.Search).assertIsDisplayed()
        composeRule.waitUntil(timeoutMillis = 5_000) {
            composeRule.onAllNodesWithText(emptyTitle).fetchSemanticsNodes().isNotEmpty()
        }
        composeRule.onAllNodesWithText(emptyTitle)[0].assertIsDisplayed()
        composeRule.onAllNodesWithText(emptyMessage)[0].assertIsDisplayed()
    }

    @Test
    fun homeShowsOnlyTodayFeaturedAndNewTracks() {
        composeRule.setHomeContent()

        composeRule.onNodeWithText(context.getString(R.string.home_featured)).assertIsDisplayed()
        composeRule.onNodeWithText(context.getString(R.string.home_new_tracks)).assertIsDisplayed()
        composeRule.onAllNodesWithText(context.getString(R.string.search_view_all)).assertCountEquals(0)
        composeRule.runOnIdle {
            assertThat(composeRule.onAllNodesWithText("新碟上架").fetchSemanticsNodes()).isEmpty()
            assertThat(composeRule.onAllNodesWithText("歌手精选").fetchSemanticsNodes()).isEmpty()
        }
    }

    @Test
    @Config(qualifiers = "w900dp-h420dp-land")
    fun landscapeHomeShowsFeaturedAndRecommendationsSideBySide() {
        composeRule.setHomeContent()

        val featuredBounds =
            composeRule.onNodeWithTag(
                HomeTestTags.LandscapeFeaturedPane,
            ).assertIsDisplayed().fetchSemanticsNode().boundsInRoot
        val recommendedBounds =
            composeRule
                .onNodeWithTag(HomeTestTags.LandscapeRecommendedPane)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot

        assertThat(featuredBounds.right).isLessThan(recommendedBounds.left)
        composeRule.onNodeWithTag(HomeTestTags.featuredAlbum("album-1")).assertIsDisplayed()
        composeRule.onNodeWithText("Track One").assertIsDisplayed()
    }

    private fun ComposeContentTestRule.setHomeContent(
        tracks: List<CatalogTrackUi> =
            listOf(
                track("track-1", "Track One"),
                track("track-2", "Track Two"),
                track("track-3", "Track Three"),
            ),
        featuredAlbums: List<CatalogAlbumUi> =
            listOf(
                album("album-1", "Album One"),
                album("album-2", "Album Two"),
            ),
        recommendedTracks: List<CatalogTrackUi> = tracks,
        featuredLoading: Boolean = false,
        recommendedLoading: Boolean = false,
        featuredFailed: Boolean = false,
        recommendedFailed: Boolean = false,
        profileUiState: HomeProfileUiState = HomeProfileUiState(),
        onSearchClick: () -> Unit = {},
        onProfileClick: () -> Unit = {},
        onAlbumClick: (String) -> Unit = {},
        onTrackPlay: (CatalogTrackUi) -> Unit = {},
        onRetryFeatured: () -> Unit = {},
        onRetryRecommended: () -> Unit = {},
    ) {
        setContent {
            XyMusicTheme(darkTheme = false) {
                HomeContent(
                    randomUiState =
                    CatalogRandomUiState(
                        featuredAlbums = featuredAlbums,
                        recommendedTracks = recommendedTracks,
                        featuredLoading = featuredLoading,
                        recommendedLoading = recommendedLoading,
                        featuredFailed = featuredFailed,
                        recommendedFailed = recommendedFailed,
                    ),
                    profileUiState = profileUiState,
                    onSearchClick = onSearchClick,
                    onProfileClick = onProfileClick,
                    onAlbumClick = onAlbumClick,
                    onRecommendedTrackPlay = onTrackPlay,
                    onTrackMore = {},
                    onRetryFeatured = onRetryFeatured,
                    onRetryRecommended = onRetryRecommended,
                )
            }
        }
    }

    private fun album(id: String, title: String) = CatalogAlbumUi(
        id = id,
        title = title,
        artists = listOf(CatalogArtistLinkUi("artist-1", "Artist One")),
        cover = null,
        releaseDate = "2026-01-01",
        trackCount = 10,
    )

    private fun track(id: String, title: String) = CatalogTrackUi(
        id = id,
        title = title,
        artists = listOf(CatalogArtistLinkUi("artist-1", "Artist One")),
        album = CatalogAlbumLinkUi("album-1", "Album One"),
        artwork = null,
        durationMs = 185_000,
        discNumber = 1,
        trackNumber = 1,
    )
}
