package com.xymusic.app.feature.server.presentation

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTextClearance
import androidx.compose.ui.test.performTextInput
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.network.ServerEndpoint
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMusicTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ComposeTestApplication::class)
class ServerConfigUiTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun initialSetupCollectsHostAndPort() {
        var saved: ServerEndpoint? = null
        composeRule.setContent {
            XyMusicTheme(darkTheme = false) {
                ServerSetupScreen(
                    onSave = { saved = it },
                )
            }
        }

        composeRule.onNodeWithTag(ServerConfigTestTags.Host).performTextInput("192.168.1.20")
        composeRule.onNodeWithTag(ServerConfigTestTags.Port).performTextInput("3000")
        composeRule.onNodeWithTag(ServerConfigTestTags.Save).performClick()

        assertThat(saved?.displayValue).isEqualTo("https://192.168.1.20:3000")
    }

    @Test
    fun initialSetupAllowsExplicitHttp() {
        var saved: ServerEndpoint? = null
        composeRule.setContent {
            XyMusicTheme(darkTheme = false) {
                ServerSetupScreen(
                    onSave = { saved = it },
                )
            }
        }

        composeRule.onNodeWithTag(ServerConfigTestTags.Host).performTextInput("192.168.1.20")
        composeRule.onNodeWithTag(ServerConfigTestTags.Port).performTextInput("3000")
        composeRule.onNodeWithTag(ServerConfigTestTags.Https).performClick()
        composeRule.onNodeWithTag(ServerConfigTestTags.Save).performClick()

        assertThat(saved).isNull()
        composeRule.onNodeWithTag(ServerConfigTestTags.ConfirmHttp).performClick()

        assertThat(saved?.displayValue).isEqualTo("http://192.168.1.20:3000")
    }

    @Test
    fun initialSetupRejectsInvalidEndpointAndShowsError() {
        var saved: ServerEndpoint? = null
        composeRule.setContent {
            XyMusicTheme(darkTheme = false) {
                ServerSetupScreen(
                    onSave = { saved = it },
                )
            }
        }

        composeRule.onNodeWithTag(ServerConfigTestTags.Host).performTextInput("192.168.1.20")
        composeRule.onNodeWithTag(ServerConfigTestTags.Port).performTextInput("70000")
        composeRule.onNodeWithTag(ServerConfigTestTags.Save).performClick()

        assertThat(saved).isNull()
        composeRule.onNodeWithText("请输入有效的 IP/主机名和 1–65535 端口").assertIsDisplayed()
    }

    @Test
    fun editDialogReturnsModifiedEndpoint() {
        val initial = checkNotNull(ServerEndpoint.parse("music.home", "3000"))
        var saved: ServerEndpoint? = null
        composeRule.setContent {
            XyMusicTheme(darkTheme = false) {
                ServerEndpointDialog(
                    currentEndpoint = initial,
                    onDismiss = {},
                    onSave = { saved = it },
                )
            }
        }

        composeRule.onNodeWithTag(ServerConfigTestTags.Port).performTextClearance()
        composeRule.onNodeWithTag(ServerConfigTestTags.Port).performTextInput("4000")
        composeRule.onNodeWithTag(ServerConfigTestTags.Save).performClick()

        assertThat(saved?.displayValue).isEqualTo("https://music.home:4000")
    }

    @Test
    @Config(qualifiers = "w740dp-h320dp-land")
    fun compactLandscapeSetupPlacesIntroductionBesideScrollableForm() {
        composeRule.setContent {
            XyMusicTheme(darkTheme = false) {
                ServerSetupScreen(onSave = {})
            }
        }

        val introBounds =
            composeRule
                .onNodeWithTag(ServerConfigTestTags.LandscapeIntro)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot
        val formBounds =
            composeRule
                .onNodeWithTag(ServerConfigTestTags.LandscapeForm)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot

        assertThat(introBounds.right).isLessThan(formBounds.left)
        composeRule.onNodeWithTag(ServerConfigTestTags.Port).performScrollTo().assertIsDisplayed()
        composeRule.onNodeWithTag(ServerConfigTestTags.Save).performScrollTo().assertIsDisplayed()
    }

    @Test
    @Config(qualifiers = "w740dp-h320dp-land")
    fun compactLandscapeEditDialogKeepsFieldsAndConfirmationReachable() {
        val initial = checkNotNull(ServerEndpoint.parse("music.home", "3000"))
        composeRule.setContent {
            XyMusicTheme(darkTheme = false) {
                ServerEndpointDialog(
                    currentEndpoint = initial,
                    onDismiss = {},
                    onSave = {},
                )
            }
        }

        val hostBounds =
            composeRule
                .onNodeWithTag(ServerConfigTestTags.Host)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot
        val portBounds =
            composeRule
                .onNodeWithTag(ServerConfigTestTags.Port)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot

        assertThat(hostBounds.right).isLessThan(portBounds.left)
        composeRule.onNodeWithTag(ServerConfigTestTags.Save).assertIsDisplayed()
    }
}
