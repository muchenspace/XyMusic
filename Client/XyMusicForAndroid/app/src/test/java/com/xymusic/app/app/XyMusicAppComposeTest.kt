package com.xymusic.app.app

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import com.xymusic.app.feature.auth.presentation.AuthEntryScreen
import com.xymusic.app.feature.auth.presentation.AuthUiState
import com.xymusic.app.feature.auth.presentation.SignInScreen
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMusicTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ComposeTestApplication::class)
class XyMusicAppComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun authenticationEntryOpensSignInForm() {
        composeRule.setContent {
            XyMusicTheme(darkTheme = false) {
                var showSignIn by remember { mutableStateOf(false) }
                if (showSignIn) {
                    SignInScreen(
                        uiState = AuthUiState(),
                        onBack = { showSignIn = false },
                        onSubmit = { _, _ -> },
                        onFieldChanged = {},
                    )
                } else {
                    AuthEntryScreen(
                        onSignIn = { showSignIn = true },
                        onRegister = {},
                    )
                }
            }
        }

        composeRule.onNodeWithText("XyMusic").assertIsDisplayed()
        composeRule.onNodeWithText("登录").performClick()
        composeRule.onNodeWithText("欢迎回来").assertIsDisplayed()
    }
}
