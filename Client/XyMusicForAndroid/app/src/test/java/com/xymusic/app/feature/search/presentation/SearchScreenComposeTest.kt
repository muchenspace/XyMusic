package com.xymusic.app.feature.search.presentation

import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performImeAction
import androidx.paging.PagingData
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.ui.media.CatalogAlbumLinkUi
import com.xymusic.app.core.ui.media.CatalogArtistLinkUi
import com.xymusic.app.core.ui.media.CatalogTrackUi
import com.xymusic.app.feature.search.domain.model.SearchScope
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
class SearchScreenComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun idleSearchShowsBrowseCategoriesAndSelectsScope() {
        var selectedScope: SearchScope? = null
        composeRule.setSearchContent(
            state = SearchUiState(),
            onScopeSelected = { selectedScope = it },
        )

        composeRule.onNodeWithText("浏览分类").assertIsDisplayed()
        composeRule.onAllNodesWithText("专辑")[0].performClick()

        assertThat(selectedScope).isEqualTo(SearchScope.ALBUMS)
    }

    @Test
    fun imeSearchSubmitsImmediatelyAndHistorySupportsDeleteAndClear() {
        var submitted = false
        var selected: SearchHistoryUi? = null
        var deleted: SearchHistoryUi? = null
        var cleared = false
        val historyItem = SearchHistoryUi("First Track", SearchScope.TRACKS, "2026/7/11 10:00")
        composeRule.setSearchContent(
            state = SearchUiState(input = "First Track", history = listOf(historyItem)),
            onSubmit = { submitted = true },
            onHistorySelected = { selected = it },
            onDeleteHistory = { deleted = it },
            onClearHistory = { cleared = true },
        )

        composeRule.onNodeWithTag(SearchTestTags.Input).performImeAction()
        composeRule.onNodeWithTag(SearchTestTags.historyItem(historyItem)).performClick()
        composeRule.onNodeWithContentDescription("删除搜索记录").performClick()
        composeRule.onNodeWithTag(SearchTestTags.ClearHistory).performClick()
        composeRule.onNodeWithText("确认").performClick()

        assertThat(submitted).isTrue()
        assertThat(selected).isEqualTo(historyItem)
        assertThat(deleted).isEqualTo(historyItem)
        assertThat(cleared).isTrue()
    }

    @Test
    fun allOverviewShowsCachedSectionsAndPlaysTrackResult() {
        var playedTrack: String? = null
        var retried = false
        composeRule.setSearchContent(
            state =
            SearchUiState(
                input = "First",
                activeQuery = "First",
                overview =
                SearchOverviewUi(
                    tracks = listOf(track()),
                    artists = emptyList(),
                    albums = emptyList(),
                ),
                overviewRefreshFailed = true,
            ),
            onSubmit = { retried = true },
            onTrackPlay = { playedTrack = it.id },
        )

        composeRule.onNodeWithText("无法更新，正在显示缓存内容").assertIsDisplayed()
        composeRule.onNodeWithText("刷新").performClick()
        composeRule.onNodeWithText("First Track").performClick()

        assertThat(retried).isTrue()
        assertThat(playedTrack).isEqualTo("track-1")
    }

    @Test
    fun searchScopeTransitionKeepsTheOutgoingResultsComposed() {
        val uiState =
            mutableStateOf(
                SearchUiState(
                    input = "First",
                    activeQuery = "First",
                    overview =
                    SearchOverviewUi(
                        tracks = listOf(track(title = "Overview Track")),
                        artists = emptyList(),
                        albums = emptyList(),
                    ),
                ),
            )
        val scopeTracks = flowOf(PagingData.from(listOf(track(title = "Scoped Track"))))
        composeRule.setContent {
            XyMusicTheme(darkTheme = false) {
                SearchContent(
                    uiState = uiState.value,
                    tracks = scopeTracks,
                    artists = flowOf(PagingData.empty()),
                    albums = flowOf(PagingData.empty()),
                    snackbarHostState = remember { SnackbarHostState() },
                    onQueryChanged = {},
                    onSubmit = {},
                    onClearQuery = {},
                    onScopeSelected = { scope ->
                        uiState.value = uiState.value.copy(selectedScope = scope)
                    },
                    onHistorySelected = {},
                    onDeleteHistory = {},
                    onClearHistory = {},
                    onAlbumClick = {},
                    onArtistClick = {},
                    onTrackPlay = { _, _ -> },
                    onTrackMore = {},
                )
            }
        }

        composeRule.waitForIdle()
        composeRule.mainClock.autoAdvance = false
        try {
            composeRule.onNodeWithTag(SearchTestTags.scope(SearchScope.TRACKS)).performClick()
            composeRule.mainClock.advanceTimeByFrame()
            composeRule.mainClock.advanceTimeByFrame()
            composeRule.waitForIdle()

            composeRule.onNodeWithText("Overview Track").assertExists()
        } finally {
            composeRule.mainClock.autoAdvance = true
            composeRule.waitForIdle()
        }
        composeRule.onNodeWithText("Scoped Track").assertIsDisplayed()
    }

    @Test
    fun secondarySearchBackInvokesCallback() {
        var backed = false
        composeRule.setSearchContent(
            state = SearchUiState(),
            onBack = { backed = true },
        )

        composeRule.onNodeWithContentDescription("返回").performClick()

        assertThat(backed).isTrue()
    }

    @Test
    fun invalidQueryDisplaysLengthError() {
        composeRule.setSearchContent(
            state =
            SearchUiState(
                input = "x".repeat(201),
                queryError = SearchQueryErrorUi.TooLong,
            ),
        )

        composeRule.onNodeWithText("搜索内容不能超过 200 个字符").assertIsDisplayed()
    }

    @Test
    @Config(qualifiers = "w900dp-h420dp-land")
    fun landscapeSearchUsesCompactHeaderAndTwoPaneOverview() {
        composeRule.setSearchContent(
            state =
            SearchUiState(
                input = "First",
                activeQuery = "First",
                overview =
                SearchOverviewUi(
                    tracks = listOf(track()),
                    artists = emptyList(),
                    albums = emptyList(),
                ),
            ),
        )

        composeRule.onNodeWithTag(SearchTestTags.LandscapeHeader).assertIsDisplayed()
        composeRule.onNodeWithTag(SearchTestTags.Input).assertIsDisplayed()
        val tracksBounds =
            composeRule
                .onNodeWithTag(SearchTestTags.LandscapeOverviewTracks)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot
        val mediaBounds =
            composeRule
                .onNodeWithTag(SearchTestTags.LandscapeOverviewMedia)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot

        assertThat(tracksBounds.right).isLessThan(mediaBounds.left)
        composeRule.onNodeWithText("First Track").assertIsDisplayed()
    }

    private fun androidx.compose.ui.test.junit4.ComposeContentTestRule.setSearchContent(
        state: SearchUiState,
        onSubmit: () -> Unit = {},
        onHistorySelected: (SearchHistoryUi) -> Unit = {},
        onDeleteHistory: (SearchHistoryUi) -> Unit = {},
        onClearHistory: () -> Unit = {},
        onTrackPlay: (CatalogTrackUi) -> Unit = {},
        onScopeSelected: (SearchScope) -> Unit = {},
        onBack: (() -> Unit)? = null,
    ) {
        setContent {
            XyMusicTheme(darkTheme = false) {
                SearchContent(
                    uiState = state,
                    tracks = flowOf(PagingData.empty()),
                    artists = flowOf(PagingData.empty()),
                    albums = flowOf(PagingData.empty()),
                    snackbarHostState = SnackbarHostState(),
                    onQueryChanged = {},
                    onSubmit = onSubmit,
                    onClearQuery = {},
                    onScopeSelected = onScopeSelected,
                    onHistorySelected = onHistorySelected,
                    onDeleteHistory = onDeleteHistory,
                    onClearHistory = onClearHistory,
                    onAlbumClick = {},
                    onArtistClick = {},
                    onTrackPlay = { _, track -> onTrackPlay(track) },
                    onTrackMore = {},
                    onBack = onBack,
                )
            }
        }
    }

    private fun track(title: String = "First Track") = CatalogTrackUi(
        id = "track-1",
        title = title,
        artists = listOf(CatalogArtistLinkUi("artist-1", "First Artist")),
        album = CatalogAlbumLinkUi("album-1", "First Album"),
        artwork = null,
        durationMs = 180_000,
        discNumber = 1,
        trackNumber = 1,
    )
}
