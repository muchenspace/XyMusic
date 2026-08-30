package com.xymusic.app

import android.content.Context
import android.os.ParcelFileDescriptor
import android.os.SystemClock
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import androidx.compose.ui.test.onAllNodesWithContentDescription
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onFirst
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performImeAction
import androidx.compose.ui.test.performScrollToIndex
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.test.printToLog
import androidx.compose.ui.test.swipeLeft
import androidx.compose.ui.test.swipeRight
import androidx.compose.ui.test.swipeUp
import androidx.test.core.app.ActivityScenario
import androidx.test.espresso.Espresso.pressBack
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import com.xymusic.app.feature.player.presentation.PlayerTestTags
import java.net.InetAddress
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.util.Locale
import java.util.concurrent.TimeUnit
import java.util.regex.Pattern
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import okio.Buffer
import org.junit.After
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class MainNavigationPerformanceDeviceTest {
    @get:Rule
    val composeRule = createEmptyComposeRule()

    private lateinit var server: MockWebServer
    private lateinit var scenario: ActivityScenario<MainActivity>

    @Before
    fun setUp() {
        server = MockWebServer()
        server.dispatcher = PerformanceFixtureDispatcher(server)
        server.start(InetAddress.getByName(LOOPBACK_HOST), 0)

        configureLoopbackEndpoint()
        scenario = ActivityScenario.launch(MainActivity::class.java)
        signIn()
    }

    @After
    fun tearDown() {
        runShell("input keyevent KEYCODE_MEDIA_PAUSE")
        if (::scenario.isInitialized) scenario.close()
        if (::server.isInitialized) server.shutdown()
    }

    @Test
    fun mainPagesAndInteractionsRemainMeasurableOnDevice() {
        measure("home_recommendations_swipe") {
            composeRule
                .onNodeWithTag(HOME_RECOMMENDATIONS_PAGER)
                .performTouchInput { swipeLeft() }
        }

        measure("home_to_album_detail") {
            composeRule.onNodeWithTag(HOME_FEATURED_ALBUM).performClick()
            waitForText(ALBUM_TITLE)
        }
        measure("album_to_artist_detail") {
            composeRule
                .onAllNodesWithText(ARTIST_TITLE, substring = true, useUnmergedTree = true)
                .onFirst()
                .performClick()
            waitForText(ARTIST_DESCRIPTION, timeoutMillis = LONG_WAIT_MS)
        }
        measure("artist_detail_tracks") {
            composeRule.onNodeWithTag(ARTIST_TRACKS_TAB).performClick()
            waitForText(TRACK_TITLE_1, timeoutMillis = LONG_WAIT_MS)
        }
        measure("artist_detail_scroll") {
            swipeList(CATALOG_ARTIST_TRACKS_LIST)
        }
        pressBack()
        waitForText(ALBUM_TITLE, timeoutMillis = LONG_WAIT_MS)
        measure("album_detail_scroll") {
            swipeList(CATALOG_ALBUM_TRACKS_LIST)
        }
        pressBack()
        waitForTag(HOME_DISCOVER_LIST)

        measure("home_to_search") {
            composeRule.onNodeWithTag(HOME_SEARCH).performClick()
            waitForTag(SEARCH_INPUT)
        }
        measure("search_submit") {
            composeRule.onNodeWithTag(SEARCH_INPUT).performTextInput(SEARCH_QUERY)
            composeRule.onNodeWithTag(SEARCH_INPUT).performImeAction()
        }
        waitForText(TRACK_TITLE_1, timeoutMillis = LONG_WAIT_MS)
        measure("search_overview_scroll") {
            composeRule
                .onNodeWithTag(SEARCH_RESULTS)
                .performTouchInput {
                    repeat(3) { swipeUp(durationMillis = SWIPE_DURATION_MS) }
                }
        }
        measure("search_tracks_scope") {
            composeRule.onNodeWithTag(SEARCH_SCOPE_TRACKS).performClick()
            waitForText(TRACK_TITLE_1, timeoutMillis = LONG_WAIT_MS)
        }
        measure("search_tracks_scroll") {
            swipeList(SEARCH_TRACKS_LIST)
        }
        measure("search_artists_scope") {
            composeRule.onNodeWithTag(SEARCH_SCOPE_ARTISTS).performClick()
            waitForText(ARTIST_TITLE, timeoutMillis = LONG_WAIT_MS)
        }
        measure("search_artists_scroll") {
            swipeList(SEARCH_ARTISTS_LIST)
        }
        measure("search_albums_scope") {
            composeRule.onNodeWithTag(SEARCH_SCOPE_ALBUMS).performClick()
            waitForText(ALBUM_TITLE, timeoutMillis = LONG_WAIT_MS)
        }
        measure("search_albums_scroll") {
            swipeList(SEARCH_ALBUMS_LIST)
        }
        pressBack()
        composeRule.waitForIdle()
        if (hasTag(SEARCH_INPUT)) pressBack()
        waitForTag(HOME_DISCOVER_LIST)

        measure("home_to_mine") {
            composeRule.onNodeWithTag(NAVIGATION_MINE).performClick()
            waitForTag(MINE_ROOT)
            waitForText(PLAYLIST_TITLE)
        }
        measure("mine_playlist_row_swipe") {
            composeRule
                .onNodeWithTag(MINE_PLAYLISTS_ROW)
                .performTouchInput { swipeLeft() }
        }
        composeRule
            .onNodeWithTag(MINE_PLAYLISTS_ROW)
            .performTouchInput { swipeRight() }
        composeRule.waitForIdle()

        measure("mine_to_library_favorites") {
            composeRule.onNodeWithTag(MINE_FAVORITES).performClick()
            waitForText(TRACK_TITLE_1)
        }
        measure("library_favorites_scroll") {
            composeRule
                .onNodeWithTag(LIBRARY_FAVORITES_LIST)
                .performTouchInput { swipeUp(durationMillis = SCROLL_DURATION_MS) }
        }
        pressBack()
        waitForTag(MINE_ROOT)

        measure("mine_to_library_playlists") {
            composeRule.onNodeWithTag(MINE_PLAYLISTS).performClick()
            waitForText(PLAYLIST_TITLE)
        }
        measure("library_playlists_scroll") {
            swipeList(LIBRARY_PLAYLISTS_LIST)
        }
        pressBack()
        waitForTag(MINE_ROOT)

        measure("mine_to_playlist_detail") {
            composeRule.onNodeWithTag(MINE_PLAYLIST).performClick()
            waitForText(PLAYLIST_TITLE)
            waitForText(TRACK_TITLE_1)
        }
        measure("playlist_detail_scroll") {
            swipeList(PLAYLIST_TRACKS_LIST)
        }
        pressBack()
        waitForTag(MINE_ROOT)

        measure("mine_to_settings") {
            composeRule.onNodeWithTag(MINE_SETTINGS).performClick()
            waitForTag(SETTINGS_ROOT)
        }
        SETTINGS_PAGES.forEach { page ->
            measure("settings_${page.tag}") {
                composeRule.onNodeWithTag(page.tag).performClick()
                waitForText(targetContext().getString(page.titleRes), timeoutMillis = LONG_WAIT_MS)
            }
            composeRule
                .onNodeWithContentDescription(targetContext().getString(R.string.common_back))
                .performClick()
            composeRule.waitForIdle()
            waitForTag(SETTINGS_ROOT, timeoutMillis = LONG_WAIT_MS)
        }
        measure("settings_scroll") {
            composeRule
                .onNodeWithTag(SETTINGS_ROOT)
                .performTouchInput {
                    repeat(2) { swipeUp(durationMillis = SWIPE_DURATION_MS) }
                }
        }
        pressBack()
        waitForTag(MINE_ROOT)

        measure("mine_to_home") {
            composeRule.onNodeWithTag(NAVIGATION_HOME).performClick()
            waitForTag(HOME_DISCOVER_LIST)
        }
        composeRule
            .onNodeWithTag(HOME_RECOMMENDATIONS_PAGER)
            .performTouchInput { swipeRight() }
        composeRule.waitForIdle()
        waitForText(TRACK_TITLE_1, timeoutMillis = LONG_WAIT_MS)
        measure("home_to_player") {
            composeRule
                .onNodeWithContentDescription(
                    targetContext().getString(R.string.player_play_track, TRACK_TITLE_1),
                    useUnmergedTree = true,
                ).performClick()
            // Pause before querying the semantics tree. The fixture player becomes ready
            // asynchronously, so repeat the idempotent pause command until the clock stops.
            repeat(12) {
                SystemClock.sleep(250L)
                runShell("input keyevent KEYCODE_MEDIA_PAUSE")
            }
            waitForTag(PLAYER_MINI_BAR)
            SystemClock.sleep(200L)
            composeRule.onNodeWithTag(PLAYER_OPEN).performClick()
            waitForTag(PLAYER_TOP_BAR)
        }
        measure("player_lyrics_and_queue") {
            waitForTag(PLAYER_CONTENT_PAGER)
            composeRule.onNodeWithTag(PLAYER_CONTENT_PAGER).performTouchInput { swipeLeft() }
            waitForText(LYRIC_TITLE_1)
            composeRule.onNodeWithTag(PLAYER_CONTENT_PAGER).performTouchInput { swipeLeft() }
            waitForTag(PLAYER_QUEUE_CONTENT)
        }
        composeRule.waitForIdle()
        measure("player_lyrics_scroll") {
            composeRule.onNodeWithTag(PLAYER_CONTENT_PAGER).performTouchInput { swipeRight() }
            waitForText(LYRIC_TITLE_1)
            composeRule.onNodeWithTag(PLAYER_CONTENT_PAGER).performTouchInput {
                repeat(3) { swipeUp(durationMillis = SWIPE_DURATION_MS) }
            }
        }
        resetScrollableToTop(PLAYER_LYRICS_LIST)
        scrollToText(LYRIC_TITLE_20, PLAYER_LYRICS_LIST, targetIndex = 19)
        composeRule.waitForIdle()
        pausePlayback()
        measure("player_lyrics_seek_first_transition") {
            composeRule
                .onNodeWithTag(PlayerTestTags.lyricLine(19), useUnmergedTree = true)
                .performClick()
            waitForSelectedText(LYRIC_TITLE_20)
            composeRule.waitUntil(LONG_WAIT_MS) {
                hasContentDescription(targetContext().getString(R.string.player_play))
            }
            composeRule.onNodeWithTag(PLAYER_TOGGLE).performClick()
            SystemClock.sleep(850L)
            runShell("input keyevent KEYCODE_MEDIA_PAUSE")
            composeRule.waitForIdle()
            waitForSelectedText(LYRIC_TITLE_21)
        }
        measure("player_playback_toggle") {
            composeRule.onNodeWithTag(PLAYER_TOGGLE).performClick()
            SystemClock.sleep(500L)
            runShell("input keyevent KEYCODE_MEDIA_PAUSE")
        }
    }

    private fun signIn() {
        composeRule.waitUntil(LONG_WAIT_MS) {
            hasTag(AUTH_ENTRY_SIGN_IN) ||
                hasTag(AUTH_USERNAME) ||
                hasTag(HOME_DISCOVER_LIST) ||
                hasTag(NAVIGATION_HOME)
        }
        when {
            hasTag(HOME_DISCOVER_LIST) -> Unit
            hasTag(NAVIGATION_HOME) -> {
                composeRule.onNodeWithTag(NAVIGATION_HOME).performClick()
                waitForTag(HOME_DISCOVER_LIST, timeoutMillis = LONG_WAIT_MS)
            }
            else -> {
                if (hasTag(AUTH_ENTRY_SIGN_IN)) {
                    composeRule.onNodeWithTag(AUTH_ENTRY_SIGN_IN).performClick()
                    waitForTag(AUTH_USERNAME)
                }
                submitCredentials()
            }
        }
        waitForTag(HOME_DISCOVER_LIST, timeoutMillis = LONG_WAIT_MS)
        waitForText(TRACK_TITLE_1, timeoutMillis = LONG_WAIT_MS)
    }

    private fun submitCredentials() {
        composeRule.onNodeWithTag(AUTH_USERNAME).performTextInput(USERNAME)
        composeRule.onNodeWithTag(AUTH_PASSWORD).performTextInput(PASSWORD)
        composeRule.onNodeWithTag(AUTH_SUBMIT).performClick()
    }

    private fun measure(label: String, action: () -> Unit) {
        runShell("dumpsys gfxinfo ${targetPackage()} reset")
        val startedAt = SystemClock.elapsedRealtime()
        action()
        composeRule.waitForIdle()
        SystemClock.sleep(SETTLE_TIME_MS)
        val report = runShell("dumpsys gfxinfo ${targetPackage()}")
        val metrics = GfxMetrics.from(report)
        println(
            "XyMusicPerf scenario=$label totalFrames=${metrics.totalFrames} " +
                "jankyFrames=${metrics.jankyFrames} missedVsync=${metrics.missedVsync} " +
                "elapsedMs=${SystemClock.elapsedRealtime() - startedAt}",
        )
    }

    private fun waitForTag(tag: String, timeoutMillis: Long = DEFAULT_WAIT_MS) {
        try {
            composeRule.waitUntil(timeoutMillis) {
                hasTag(tag)
            }
        } catch (failure: Throwable) {
            logFixtureRequests("waitForTag:$tag")
            composeRule.onRoot(useUnmergedTree = true).printToLog("XyMusicPerf-tag-$tag")
            throw failure
        }
    }

    private fun waitForText(text: String, timeoutMillis: Long = DEFAULT_WAIT_MS) {
        try {
            composeRule.waitUntil(timeoutMillis) {
                isTextVisible(text)
            }
        } catch (failure: Throwable) {
            logFixtureRequests("waitForText:$text")
            composeRule.onRoot(useUnmergedTree = true).printToLog("XyMusicPerf-$text")
            throw failure
        }
    }

    private fun scrollToText(expectedText: String, listTag: String, targetIndex: Int) {
        val scrollable = composeRule.onNodeWithTag(listTag)
        if (hasText(expectedText)) return
        for (index in (targetIndex - 2)..(targetIndex + 2)) {
            if (index < 0) continue
            runCatching {
                scrollable.performScrollToIndex(index)
                composeRule.waitForIdle()
            }
            if (hasText(expectedText)) return
        }
        if (!hasText(expectedText)) {
            logFixtureRequests("scrollToText:$expectedText")
            composeRule.onRoot(useUnmergedTree = true).printToLog("XyMusicPerf-$expectedText")
            throw AssertionError("Expected text '$expectedText' near index $targetIndex")
        }
    }

    private fun swipeList(listTag: String, repeatCount: Int = 4) {
        composeRule.onNodeWithTag(listTag).performTouchInput {
            repeat(repeatCount) { swipeUp(durationMillis = SCROLL_DURATION_MS) }
        }
    }

    private fun resetScrollableToTop(listTag: String) {
        composeRule
            .onNodeWithTag(listTag)
            .performScrollToIndex(0)
        composeRule.waitForIdle()
    }

    private fun hasText(text: String): Boolean = isTextVisible(text)

    private fun pausePlayback() {
        repeat(3) {
            runShell("input keyevent KEYCODE_MEDIA_PAUSE")
            SystemClock.sleep(100L)
        }
        composeRule.waitUntil(LONG_WAIT_MS) {
            hasContentDescription(targetContext().getString(R.string.player_play))
        }
    }

    private fun hasSelectedText(text: String): Boolean = runCatching {
        val node = composeRule
            .onNodeWithTag(PlayerTestTags.lyricLine(lyricIndex(text)))
            .fetchSemanticsNode()
        node.config.contains(androidx.compose.ui.semantics.SemanticsProperties.Selected) &&
            node.config[androidx.compose.ui.semantics.SemanticsProperties.Selected]
    }.getOrDefault(false)

    private fun lyricIndex(text: String): Int = text.substringAfterLast(' ').toInt() - 1

    private fun waitForSelectedText(text: String) {
        composeRule.waitUntil(LONG_WAIT_MS) { hasSelectedText(text) }
    }

    private fun isTextVisible(text: String): Boolean = runCatching {
        composeRule
            .onAllNodesWithText(text, substring = true, useUnmergedTree = true)
            .fetchSemanticsNodes()
            .isNotEmpty()
    }.getOrDefault(false)

    private fun hasContentDescription(description: String): Boolean = runCatching {
        composeRule
            .onAllNodesWithContentDescription(description, useUnmergedTree = true)
            .fetchSemanticsNodes()
            .isNotEmpty()
    }.getOrDefault(false)

    private fun logFixtureRequests(label: String) {
        println("XyMusicPerf diagnostic=$label requestCount=${server.requestCount}")
        while (true) {
            val request = server.takeRequest(0, TimeUnit.MILLISECONDS) ?: break
            println("XyMusicPerf request=${request.method} ${request.requestUrl}")
        }
    }

    private fun configureLoopbackEndpoint() {
        targetContext()
            .getSharedPreferences(SERVER_PREFERENCES, Context.MODE_PRIVATE)
            .edit()
            .putString(SERVER_PROTOCOL_KEY, HTTP_PROTOCOL)
            .putString(SERVER_HOST_KEY, LOOPBACK_HOST)
            .putInt(SERVER_PORT_KEY, server.port)
            .commit()
    }

    private fun hasTag(tag: String): Boolean = runCatching {
        composeRule
            .onAllNodesWithTag(tag, useUnmergedTree = true)
            .fetchSemanticsNodes()
            .isNotEmpty()
    }.getOrDefault(false)

    private fun targetContext(): Context = InstrumentationRegistry.getInstrumentation().targetContext

    private fun targetPackage(): String = targetContext().packageName

    private fun runShell(command: String): String {
        val descriptor =
            InstrumentationRegistry
                .getInstrumentation()
                .uiAutomation
                .executeShellCommand(command)
        return ParcelFileDescriptor.AutoCloseInputStream(descriptor).bufferedReader().use { it.readText() }
    }

    private data class GfxMetrics(val totalFrames: Int?, val jankyFrames: Int?, val missedVsync: Int?) {
        companion object {
            fun from(report: String): GfxMetrics = GfxMetrics(
                totalFrames = report.metric("Total frames rendered"),
                jankyFrames = report.metric("Janky frames"),
                missedVsync = report.metric("Number Missed Vsync"),
            )

            private fun String.metric(label: String): Int? = Pattern
                .compile("${Pattern.quote(label)}\\s*:\\s*(\\d+)")
                .matcher(this)
                .takeIf { it.find() }
                ?.group(1)
                ?.toIntOrNull()
        }
    }

    private data class SettingsPageProbe(val tag: String, val titleRes: Int)

    private companion object {
        const val LOOPBACK_HOST = "127.0.0.1"
        const val HTTP_PROTOCOL = "HTTP"
        const val SERVER_PREFERENCES = "xy_music_server_config"
        const val SERVER_PROTOCOL_KEY = "protocol"
        const val SERVER_HOST_KEY = "host"
        const val SERVER_PORT_KEY = "port"
        const val USERNAME = "perfuser"
        const val PASSWORD = "performance-password"
        const val SEARCH_QUERY = "perf"
        const val SWIPE_DURATION_MS = 350L
        const val SCROLL_DURATION_MS = 180L
        const val SETTLE_TIME_MS = 100L
        const val DEFAULT_WAIT_MS = 5_000L
        const val LONG_WAIT_MS = 20_000L

        const val AUTH_ENTRY_SIGN_IN = "auth_entry_sign_in"
        const val AUTH_USERNAME = "auth_username"
        const val AUTH_PASSWORD = "auth_password"
        const val AUTH_SUBMIT = "auth_submit"
        const val HOME_DISCOVER_LIST = "home_discover_list"
        const val HOME_RECOMMENDATIONS_PAGER = "home_recommendations_pager"
        const val HOME_FEATURED_ALBUM = "home_featured_album_00000000-0000-4000-8000-0000000000c8"
        const val HOME_SEARCH = "home_search"
        const val SEARCH_INPUT = "search_input"
        const val SEARCH_RESULTS = "search_results"
        const val SEARCH_SCOPE_TRACKS = "search_scope_TRACKS"
        const val SEARCH_SCOPE_ARTISTS = "search_scope_ARTISTS"
        const val SEARCH_SCOPE_ALBUMS = "search_scope_ALBUMS"
        const val SEARCH_TRACKS_LIST = "search_result_list_TRACKS"
        const val SEARCH_ARTISTS_LIST = "search_result_list_ARTISTS"
        const val SEARCH_ALBUMS_LIST = "search_result_list_ALBUMS"
        const val NAVIGATION_HOME = "main_navigation_home"
        const val NAVIGATION_MINE = "main_navigation_mine"
        const val MINE_ROOT = "mine_root"
        const val MINE_PLAYLISTS_ROW = "mine_playlists_row"
        const val MINE_SETTINGS = "mine_settings"
        const val MINE_FAVORITES = "mine_favorites"
        const val MINE_PLAYLISTS = "mine_playlists"
        const val LIBRARY_FAVORITES_LIST = "library_favorites_list"
        const val LIBRARY_PLAYLISTS_LIST = "library_playlists_list"
        const val MINE_PLAYLIST = "mine_playlist_00000000-0000-4000-8000-000000000190"
        const val SETTINGS_ROOT = "settings_root"
        const val PLAYER_MINI_BAR = "player_mini_bar"
        const val PLAYER_OPEN = "player_open_fullscreen"
        const val PLAYER_TOP_BAR = "player_top_bar"
        const val PLAYER_CONTENT_PAGER = "player_content_pager"
        const val PLAYER_LYRICS_LIST = "player_lyrics_list"
        const val PLAYER_QUEUE_CONTENT = "player_queue_content"
        const val CATALOG_ALBUM_TRACKS_LIST = "catalog-album-tracks-list"
        const val CATALOG_ARTIST_TRACKS_LIST = "catalog-artist-tracks-list"
        const val PLAYLIST_TRACKS_LIST = "playlist-tracks-list"
        const val PLAYER_TOGGLE = "player_toggle_playback"
        const val ALBUM_TITLE = "Perf Album 1"
        const val ALBUM_TITLE_20 = "Perf Album 20"
        const val ARTIST_TITLE = "Perf Artist 1"
        const val ARTIST_TITLE_20 = "Perf Artist 20"
        const val ARTIST_DESCRIPTION = "Performance fixture artist"
        const val ARTIST_TRACKS_TAB = "catalog-artist-tab-Tracks"
        const val LYRIC_TITLE_20 = "Perf lyric 20"
        const val LYRIC_TITLE_21 = "Perf lyric 21"
        const val PLAYLIST_TITLE = "Perf Playlist 1"
        const val PLAYLIST_TITLE_3 = "Perf Playlist 3"
        const val PLAYLIST_TITLE_20 = "Perf Playlist 20"
        const val LYRIC_TITLE_1 = "Perf lyric 1"
        const val TRACK_TITLE_1 = "Perf Track 1"
        const val TRACK_TITLE_12 = "Perf Track 12"
        const val TRACK_TITLE_20 = "Perf Track 20"

        val SETTINGS_PAGES = listOf(
            SettingsPageProbe("settings_page_profile", R.string.settings_profile),
            SettingsPageProbe("settings_page_server", R.string.settings_server),
            SettingsPageProbe("settings_page_appearance", R.string.settings_appearance),
            SettingsPageProbe("settings_page_playback", R.string.settings_playback),
            SettingsPageProbe("settings_page_account", R.string.settings_account),
        )
    }
}

private class PerformanceFixtureDispatcher(private val server: MockWebServer) : Dispatcher() {
    private val audioBytes = silentWav()

    override fun dispatch(request: RecordedRequest): MockResponse {
        val path = request.requestUrl?.encodedPath ?: return notFound()
        return when {
            path == "/api/v1/auth/login" -> json(sessionResponse())
            path == "/api/v1/users/me" -> json(profileResponse())
            path == "/api/v1/tracks/random" -> json(randomTracksResponse())
            path == "/api/v1/albums/random" -> json(randomAlbumsResponse())
            path == "/api/v1/tracks" -> json(tracksPageResponse(request))
            path == "/api/v1/albums" -> json(albumsPageResponse())
            path == "/api/v1/artists" -> json(artistsPageResponse())
            path == "/api/v1/search" -> json(searchResponse(request))
            path == "/api/v1/library/favorites" -> json(favoritesResponse())
            path == "/api/v1/library/history" -> json(historyResponse())
            path == "/api/v1/playlists" -> json(playlistsResponse())
            path == "/api/v1/playlists/${playlistId()}" -> json(playlistDetailResponse())
            path.endsWith("/playback") -> json(playbackGrantResponse(path))
            path.startsWith("/api/v1/tracks/") -> json(trackDetailResponse(path.substringAfterLast('/')))
            path.startsWith("/api/v1/albums/") -> json(albumDetailResponse(path.substringAfterLast('/')))
            path.startsWith("/api/v1/artists/") -> json(artistDetailResponse(path.substringAfterLast('/')))
            path.startsWith("/audio/") -> MockResponse()
                .setResponseCode(200)
                .setHeader("Content-Type", "audio/wav")
                .setBody(Buffer().write(audioBytes))
            else -> notFound()
        }
    }

    private fun json(body: String): MockResponse = MockResponse()
        .setResponseCode(200)
        .setHeader("Content-Type", "application/json")
        .setBody(body)

    private fun notFound(): MockResponse = MockResponse()
        .setResponseCode(404)
        .setHeader("Content-Type", "application/json")
        .setBody("{}")

    private fun sessionResponse(): String = """
        {"user":{"id":"${userId()}","username":"perfuser","displayName":"Perf User","bio":"Performance fixture","avatar":null,"role":"USER","status":"ACTIVE","version":1,"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"},"session":{"id":"${sessionId()}","deviceName":"LG G8","createdAt":"2026-01-01T00:00:00Z"},"tokens":{"tokenType":"Bearer","accessToken":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","accessTokenExpiresAt":"2030-01-01T00:00:00Z","refreshToken":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","refreshTokenExpiresAt":"2031-01-01T00:00:00Z"}}
    """.trimIndent()

    private fun profileResponse(): String = """
        {"id":"${userId()}","username":"perfuser","displayName":"Perf User","bio":"Performance fixture","avatar":null,"role":"USER","status":"ACTIVE","version":1,"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}
    """.trimIndent()

    private fun randomTracksResponse(): String = "{" +
        "\"items\":[${(0 until 16).joinToString(",") { trackJson(it) }}]" +
        "}"

    private fun randomAlbumsResponse(): String = "{" +
        "\"items\":[${(0 until 2).joinToString(",") { albumJson(it) }}]" +
        "}"

    private fun tracksPageResponse(request: RecordedRequest): String {
        val requestedAlbumIndex =
            request.requestUrl
                ?.queryParameter("albumId")
                ?.let(::albumIndex)
        val itemCount = if (requestedAlbumIndex == null) 24 else 16
        val items = (0 until itemCount).joinToString(",") {
            trackJson(it, albumIndexOverride = requestedAlbumIndex)
        }
        return "{\"items\":[$items],\"nextCursor\":null}"
    }

    private fun albumsPageResponse(): String = "{" +
        "\"items\":[${(0 until 8).joinToString(",") { albumJson(it) }}],\"nextCursor\":null}"

    private fun artistsPageResponse(): String = "{" +
        "\"items\":[${(0 until 8).joinToString(",") { artistJson(it) }}],\"nextCursor\":null}"

    private fun searchResponse(request: RecordedRequest): String {
        val scope = request.requestUrl?.queryParameter("scope") ?: "ALL"
        return when (scope) {
            "TRACKS" ->
                """
                {"query":"perf","scope":"TRACKS","tracks":{"items":[${(0 until 20).joinToString(",") {
                    trackJson(it)
                }}],"nextCursor":null}}
                """.trimIndent()
            "ARTISTS" ->
                """
                {"query":"perf","scope":"ARTISTS","artists":{"items":[${(0 until 20).joinToString(",") {
                    artistJson(it)
                }}],"nextCursor":null}}
                """.trimIndent()
            "ALBUMS" ->
                """
                {"query":"perf","scope":"ALBUMS","albums":{"items":[${(0 until 20).joinToString(",") {
                    albumJson(it)
                }}],"nextCursor":null}}
                """.trimIndent()
            else ->
                """
                {"query":"perf","scope":"ALL","tracks":{"items":[${(0 until 5).joinToString(",") {
                    trackJson(it)
                }}],"nextCursor":null},"artists":{"items":[${(0 until 5).joinToString(",") {
                    artistJson(it)
                }}],"nextCursor":null},"albums":{"items":[${(0 until 5).joinToString(",") {
                    albumJson(it)
                }}],"nextCursor":null}}
                """.trimIndent()
        }
    }

    private fun favoritesResponse(): String = "{" +
        "\"items\":[${(0 until 24).joinToString(",") { index ->
            "{\"track\":${trackJson(index, favorite = true)},\"favoritedAt\":\"2026-01-01T00:00:00Z\"}"
        }}],\"nextCursor\":null}"

    private fun historyResponse(): String = "{" +
        "\"items\":[${(0 until 24).joinToString(",") { index ->
            "{\"track\":${trackJson(
                index,
            )},\"lastPositionMs\":12000,\"playCount\":2,\"lastPlayedAt\":\"2026-01-01T00:00:00Z\",\"completed\":false,\"updatedAt\":\"2026-01-01T00:00:00Z\"}"
        }}],\"nextCursor\":null}"

    private fun playlistsResponse(): String = "{" +
        "\"items\":[${(0 until 24).joinToString(",") { playlistSummaryJson(it) }}],\"nextCursor\":null}"

    private fun playlistDetailResponse(): String = """
        {"id":"${playlistId()}","owner":${userSummaryJson()},"name":"Perf Playlist 1","description":"Performance fixture playlist","visibility":"PRIVATE","cover":null,"trackCount":24,"version":1,"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z","entries":[${(0 until 24).joinToString(
        ",",
    ) {
        playlistEntryJson(it)
    }}],"nextCursor":null}
    """.trimIndent()

    private fun trackDetailResponse(requestedTrackId: String): String {
        val index = trackIndex(requestedTrackId)
        return """
            {"id":"${trackId(
            index,
        )}","title":"${trackTitle(
            index,
        )}","artists":[{"id":"${artistId(index % 3)}","name":"Perf Artist ${index % 3 + 1}"}],"album":{"id":"${albumId(
            index % 2,
        )}","title":"${albumTitle(
            index % 2,
        )}"},"artwork":null,"durationMs":180000,"trackNumber":${index + 1},"discNumber":1,"isFavorite":false,"publishedAt":"2026-01-01T00:00:00Z","lyric":{"id":"${fixtureId(
            700 + index,
        )}","trackId":"${trackId(
            index,
        )}","language":"en","format":"LRC","timing":"LINE","content":"${lyricsContent()}","trackVersion":1,"updatedAt":"2026-01-01T00:00:00Z"}}
        """.trimIndent()
    }

    private fun albumDetailResponse(requestedAlbumId: String): String {
        val index = albumIndex(requestedAlbumId)
        return """
            {"id":"${albumId(
            index,
        )}","title":"${albumTitle(
            index,
        )}","artists":[{"id":"${artistId(
            index % 3,
        )}","name":"Perf Artist ${index % 3 + 1}"}],"cover":null,"releaseDate":"2026-01-01","trackCount":16,"description":"Performance fixture album"}
        """.trimIndent()
    }

    private fun artistDetailResponse(requestedArtistId: String): String {
        val index = artistIndex(requestedArtistId)
        val artistId = artistId(index)
        val artistName = "Perf Artist ${index + 1}"
        return """
            {"id":"$artistId","name":"$artistName","artwork":null,
            "description":"Performance fixture artist"}
        """.trimIndent()
    }

    private fun playbackGrantResponse(path: String): String {
        val trackId = path.removePrefix("/api/v1/tracks/").removeSuffix("/playback")
        val sessionId = fixtureId(900)
        val audioUrl = server.url("/audio/$trackId")
        return """
            {"trackId":"$trackId","sessionId":"$sessionId","selectedQuality":"STANDARD",
            "streamUrl":"$audioUrl","expiresAt":"2030-01-01T00:00:00Z",
            "mimeType":"audio/wav","codec":"pcm_s16le","container":"wav",
            "bitrate":128000,"sampleRate":8000,"contentLength":${audioBytes.size},
            "checksumSha256":null,"cacheKey":"perf-$trackId"}
        """.trimIndent()
    }

    private fun trackJson(index: Int, favorite: Boolean = false, albumIndexOverride: Int? = null): String {
        val currentAlbumIndex = albumIndexOverride ?: index % 2
        val currentTrackId = trackId(index)
        val currentTitle = trackTitle(index)
        val currentArtistId = artistId(index % 3)
        val currentArtistName = "Perf Artist ${index % 3 + 1}"
        val currentAlbumId = albumId(currentAlbumIndex)
        val currentAlbumTitle = albumTitle(currentAlbumIndex)
        return """
            {"id":"$currentTrackId","title":"$currentTitle",
            "artists":[{"id":"$currentArtistId","name":"$currentArtistName"}],
            "album":{"id":"$currentAlbumId","title":"$currentAlbumTitle"},
            "artwork":null,"durationMs":180000,"trackNumber":${index + 1},
            "discNumber":1,"isFavorite":$favorite,"publishedAt":"2026-01-01T00:00:00Z"}
        """.trimIndent()
    }

    private fun albumJson(index: Int): String = "{\"id\":\"${albumId(
        index,
    )}\",\"title\":\"${albumTitle(
        index,
    )}\",\"artists\":[{\"id\":\"${artistId(
        index % 3,
    )}\",\"name\":\"Perf Artist ${index % 3 + 1}\"}],\"cover\":null,\"releaseDate\":\"2026-01-01\",\"trackCount\":16}"

    private fun artistJson(index: Int): String =
        "{\"id\":\"${artistId(index)}\",\"name\":\"Perf Artist ${index + 1}\",\"artwork\":null}"

    private fun playlistSummaryJson(index: Int): String {
        val currentPlaylistId = playlistId(index)
        val currentTitle = playlistTitle(index)
        return """
            {"id":"$currentPlaylistId","owner":${userSummaryJson()},"name":"$currentTitle",
            "description":"Performance fixture playlist","visibility":"PRIVATE","cover":null,
            "trackCount":24,"version":1,"createdAt":"2026-01-01T00:00:00Z",
            "updatedAt":"2026-01-01T00:00:00Z"}
        """.trimIndent()
    }

    private fun playlistEntryJson(index: Int): String = "{\"id\":\"${fixtureId(
        500 + index,
    )}\",\"position\":$index,\"track\":${trackJson(
        index,
    )},\"addedBy\":${userSummaryJson()},\"addedAt\":\"2026-01-01T00:00:00Z\"}"

    private fun userSummaryJson(): String =
        "{\"id\":\"${userId()}\",\"username\":\"perfuser\",\"displayName\":\"Perf User\",\"avatar\":null}"

    private fun lyricsContent(): String = (0 until 40).joinToString("\\n") { index ->
        val totalCentiseconds = index * 50
        String.format(
            Locale.US,
            "[%02d:%02d.%02d]Perf lyric %d",
            totalCentiseconds / 6_000,
            (totalCentiseconds / 100) % 60,
            totalCentiseconds % 100,
            index + 1,
        )
    }

    private fun silentWav(): ByteArray {
        val sampleRate = 8_000
        val channelCount = 1
        val bytesPerSample = 2
        val dataSize = sampleRate * AUDIO_DURATION_SECONDS * channelCount * bytesPerSample
        return ByteBuffer
            .allocate(44 + dataSize)
            .order(ByteOrder.LITTLE_ENDIAN)
            .put("RIFF".toByteArray(Charsets.US_ASCII))
            .putInt(36 + dataSize)
            .put("WAVE".toByteArray(Charsets.US_ASCII))
            .put("fmt ".toByteArray(Charsets.US_ASCII))
            .putInt(16)
            .putShort(1)
            .putShort(channelCount.toShort())
            .putInt(sampleRate)
            .putInt(sampleRate * channelCount * bytesPerSample)
            .putShort((channelCount * bytesPerSample).toShort())
            .putShort((bytesPerSample * 8).toShort())
            .put("data".toByteArray(Charsets.US_ASCII))
            .putInt(dataSize)
            .array()
    }

    private fun trackIndex(id: String): Int = (id.removePrefix(TRACK_ID_PREFIX).toIntOrNull(16) ?: 301) - 301

    private fun albumIndex(id: String): Int = (id.removePrefix(ALBUM_ID_PREFIX).toIntOrNull(16) ?: 200) - 200

    private fun artistIndex(id: String): Int = (id.removePrefix(ARTIST_ID_PREFIX).toIntOrNull(16) ?: 100) - 100

    private companion object {
        const val AUDIO_DURATION_SECONDS = 30
        const val TRACK_ID_PREFIX = "00000000-0000-4000-8000-"
        const val ALBUM_ID_PREFIX = TRACK_ID_PREFIX
        const val ARTIST_ID_PREFIX = TRACK_ID_PREFIX

        fun fixtureId(value: Int): String = TRACK_ID_PREFIX + String.format(Locale.US, "%012x", value)

        fun userId(): String = fixtureId(1)

        fun sessionId(): String = fixtureId(2)

        fun trackId(index: Int): String = fixtureId(301 + index)

        fun albumId(index: Int): String = fixtureId(200 + index)

        fun artistId(index: Int): String = fixtureId(100 + index)

        fun playlistId(index: Int = 0): String = fixtureId(400 + index)

        fun trackTitle(index: Int): String = "Perf Track ${index + 1}"

        fun albumTitle(index: Int): String = "Perf Album ${index + 1}"

        fun playlistTitle(index: Int): String = "Perf Playlist ${index + 1}"
    }
}
