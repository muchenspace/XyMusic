package com.xymusic.app.feature.catalog.presentation

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasScrollToIndexAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.paging.PagingData
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.ui.media.CatalogAlbumLinkUi
import com.xymusic.app.core.ui.media.CatalogAlbumUi
import com.xymusic.app.core.ui.media.CatalogArtistLinkUi
import com.xymusic.app.core.ui.media.CatalogArtistUi
import com.xymusic.app.core.ui.media.CatalogTrackUi
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMusicTheme
import kotlinx.coroutines.flow.flowOf
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ComposeTestApplication::class)
class CatalogScreensComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    /* Track detail UI was intentionally removed.
    @Test
    fun trackDetailShowsCachedLyricsAndCrossDetailLinksWithoutPlaybackActions() {
        var selectedArtist: String? = null
        var selectedAlbum: String? = null
        composeRule.setCatalogContent {
            TrackDetailScreen(
                uiState = CatalogDetailUiState(
                    item = CatalogTrackDetailUi(track(), lyricsCount = 2),
                ),
                onBack = {},
                onRefresh = {},
                onArtistClick = { selectedArtist = it },
                onAlbumClick = { selectedAlbum = it },
            )
        }

        composeRule.onNodeWithText("First Artist").performClick()
        composeRule.onNodeWithText("First Album").performClick()
        composeRule.onNode(hasScrollToIndexAction()).performScrollToNode(hasText("已缓存 2 个歌词版本"))
        composeRule.onNodeWithText("已缓存 2 个歌词版本").assertIsDisplayed()
        assertThat(composeRule.onAllNodesWithText("播放").fetchSemanticsNodes()).isEmpty()
        assertThat(composeRule.onAllNodesWithText("收藏").fetchSemanticsNodes()).isEmpty()

        assertThat(selectedArtist).isEqualTo("artist-1")
        assertThat(selectedAlbum).isEqualTo("album-1")
    }

     */
    @Test
    fun albumDetailShowsTracksInProvidedAlbumOrder() {
        composeRule.setCatalogContent {
            AlbumDetailScreen(
                uiState =
                CatalogDetailUiState(
                    item = CatalogAlbumDetailUi(album(), description = "Album description"),
                ),
                tracks =
                flowOf(
                    PagingData.from(
                        listOf(
                            track(id = "track-1", title = "Opening", trackNumber = 1),
                            track(id = "track-2", title = "Finale", trackNumber = 2),
                        ),
                    ),
                ),
                onBack = {},
                onRefresh = {},
                onTrackPlay = { _, _ -> },
                onTrackMore = {},
                onArtistClick = {},
            )
        }

        composeRule.waitForIdle()
        composeRule.onNode(hasScrollToIndexAction()).performScrollToNode(hasText("1  Opening"))
        composeRule.onNodeWithText("1  Opening").assertIsDisplayed()
        composeRule.onNode(hasScrollToIndexAction()).performScrollToNode(hasText("2  Finale"))
        composeRule.onNodeWithText("2  Finale").assertIsDisplayed()
    }

    @Test
    fun artistDetailSwitchesBetweenAlbumAndTrackCollections() {
        composeRule.setCatalogContent {
            ArtistDetailScreen(
                uiState =
                CatalogDetailUiState(
                    item = CatalogArtistDetailUi(artist(), description = "Artist description"),
                ),
                albums = flowOf(PagingData.from(listOf(album()))),
                tracks = flowOf(PagingData.from(listOf(track()))),
                onBack = {},
                onRefresh = {},
                onTrackPlay = { _, _ -> },
                onTrackMore = {},
                onAlbumClick = {},
            )
        }

        composeRule.onNode(hasScrollToIndexAction()).performScrollToNode(hasText("First Album"))
        composeRule.onNodeWithText("First Album").assertIsDisplayed()
        composeRule.onNodeWithText("歌手曲目").performClick()
        composeRule.waitForIdle()
        composeRule.onNodeWithText("First Track").assertIsDisplayed()
    }

    @Test
    @Config(qualifiers = "w740dp-h320dp-land")
    fun compactLandscapeAlbumShowsInfoBesideFirstTrack() {
        composeRule.setCatalogContent {
            AlbumDetailScreen(
                uiState =
                CatalogDetailUiState(
                    item = CatalogAlbumDetailUi(album(), description = "Album description"),
                ),
                tracks = flowOf(PagingData.from(listOf(track(title = "Opening")))),
                onBack = {},
                onRefresh = {},
                onTrackPlay = { _, _ -> },
                onTrackMore = {},
                onArtistClick = {},
            )
        }

        composeRule.waitForIdle()
        val info =
            composeRule
                .onNodeWithTag(CatalogDetailTestTags.AlbumLandscapeInfo)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot
        val content =
            composeRule
                .onNodeWithTag(CatalogDetailTestTags.LandscapeContent)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot
        val firstTrack =
            composeRule
                .onNodeWithTag(CatalogDetailTestTags.track("track-1"))
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot

        assertThat(info.right).isAtMost(content.left + 1f)
        assertThat(firstTrack.top).isAtLeast(content.top)
        assertThat(firstTrack.bottom).isAtMost(content.bottom + 1f)
    }

    @Test
    @Config(qualifiers = "w900dp-h420dp-land")
    fun wideLandscapeArtistShowsInfoBesideFirstAlbum() {
        composeRule.setCatalogContent {
            ArtistDetailScreen(
                uiState =
                CatalogDetailUiState(
                    item = CatalogArtistDetailUi(artist(), description = "Artist description"),
                ),
                albums = flowOf(PagingData.from(listOf(album()))),
                tracks = flowOf(PagingData.from(listOf(track()))),
                onBack = {},
                onRefresh = {},
                onTrackPlay = { _, _ -> },
                onTrackMore = {},
                onAlbumClick = {},
            )
        }

        composeRule.waitForIdle()
        val info =
            composeRule
                .onNodeWithTag(CatalogDetailTestTags.ArtistLandscapeInfo)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot
        val content =
            composeRule
                .onNodeWithTag(CatalogDetailTestTags.LandscapeContent)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot
        val firstAlbum =
            composeRule
                .onNodeWithTag(CatalogDetailTestTags.album("album-1"))
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot

        assertThat(info.right).isAtMost(content.left + 1f)
        assertThat(firstAlbum.top).isAtLeast(content.top)
        assertThat(firstAlbum.bottom).isAtMost(content.bottom + 1f)
    }

    private fun track(id: String = "track-1", title: String = "First Track", trackNumber: Int? = 1) = CatalogTrackUi(
        id = id,
        title = title,
        artists = listOf(CatalogArtistLinkUi("artist-1", "First Artist")),
        album = CatalogAlbumLinkUi("album-1", "First Album"),
        artwork = null,
        durationMs = 185_000,
        discNumber = 1,
        trackNumber = trackNumber,
    )

    private fun album() = CatalogAlbumUi(
        id = "album-1",
        title = "First Album",
        artists = listOf(CatalogArtistLinkUi("artist-1", "First Artist")),
        cover = null,
        releaseDate = "2026-07-11",
        trackCount = 2,
    )

    private fun artist() = CatalogArtistUi(
        id = "artist-1",
        name = "First Artist",
        artwork = null,
    )

    private fun androidx.compose.ui.test.junit4.ComposeContentTestRule.setCatalogContent(
        content: @androidx.compose.runtime.Composable () -> Unit,
    ) {
        setContent {
            XyMusicTheme(darkTheme = false, content = content)
        }
    }
}
