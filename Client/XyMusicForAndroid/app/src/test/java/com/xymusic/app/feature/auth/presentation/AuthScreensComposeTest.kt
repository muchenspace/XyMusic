package com.xymusic.app.feature.auth.presentation

import android.app.Application
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performImeAction
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTextInput
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.R
import com.xymusic.app.core.network.NetworkFailureReason
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMusicTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ComposeTestApplication::class)
class AuthScreensComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun serverValidationDetailsAreReplacedWithSafeChineseText() {
        composeRule.setAuthContent {
            SignInScreen(
                uiState =
                AuthUiState(
                    fieldErrors =
                    mapOf(
                        AuthField.Username to
                            AuthFieldError.Server(
                                "Internal validation stack trace",
                            ),
                    ),
                ),
                onBack = {},
                onSubmit = { _, _ -> },
                onFieldChanged = {},
            )
        }

        composeRule.onNodeWithText("填写内容有误，请检查后重试").assertIsDisplayed()
        assertThat(
            composeRule
                .onAllNodesWithText("Internal validation stack trace")
                .fetchSemanticsNodes(),
        ).isEmpty()
    }

    @Test
    fun internalServerDetailsUseServerErrorInsteadOfInputError() {
        val context = ApplicationProvider.getApplicationContext<Application>()

        assertThat(AuthMessage.Server("Internal SQL error").resolve(context)).isEqualTo(
            context.getString(R.string.auth_message_server_error),
        )
    }

    @Test
    fun loginFailureMessagesDescribeTheActualFailure() {
        val context = ApplicationProvider.getApplicationContext<Application>()
        val cases =
            listOf(
                AuthMessage.NetworkFailure(NetworkFailureReason.ConnectionRefused) to
                    "无法连接到服务器，请确认服务端已启动，并检查 IP 和端口",
                AuthMessage.NetworkFailure(NetworkFailureReason.HostUnresolved) to
                    "无法解析服务器地址，请检查 IP 或域名",
                AuthMessage.NetworkFailure(NetworkFailureReason.Timeout) to
                    "连接服务器超时，请检查地址、端口和网络",
                AuthMessage.NetworkFailure(NetworkFailureReason.SecureConnectionFailed) to
                    "无法建立安全连接，请检查 HTTPS 配置和证书",
                AuthMessage.NetworkFailure(NetworkFailureReason.NoRoute) to
                    "无法到达服务器，请检查手机网络、服务器地址和防火墙",
                AuthMessage.NetworkFailure(NetworkFailureReason.ConnectionLost) to
                    "与服务器的连接已中断，请确认服务端正在运行",
                AuthMessage.ServerUnavailable to "服务器暂时不可用，请确认服务端状态后重试",
                AuthMessage.LoginEndpointNotFound to "未找到登录接口，请检查服务器地址和服务端版本",
                AuthMessage.ProtocolError to "服务器响应格式不兼容，请检查服务端版本",
                AuthMessage.LocalDataError to "无法读取或保存本地登录信息，请检查设备状态后重试",
                AuthMessage.InvalidInput to "请求未通过，请检查填写内容后重试",
            )

        cases.forEach { (message, expectedText) ->
            assertThat(message.resolve(context)).isEqualTo(expectedText)
        }
    }

    @Test
    fun localizedServerMessageIsPreserved() {
        val context = ApplicationProvider.getApplicationContext<Application>()
        val message = "账号创建未完成，请稍后重试。（追踪 ID：trace-registration-error）"

        assertThat(AuthMessage.Server("  $message  ").resolve(context)).isEqualTo(message)
    }

    @Test
    fun signInDisplaysUsernameErrorsAndSubmitsEnteredCredentials() {
        var submittedUsername: String? = null
        var submittedPassword: String? = null
        var clearedField: AuthField? = null
        composeRule.setAuthContent {
            SignInScreen(
                uiState =
                AuthUiState(
                    fieldErrors = mapOf(AuthField.Username to AuthFieldError.InvalidUsername),
                ),
                onBack = {},
                onSubmit = { username, password ->
                    submittedUsername = username
                    submittedPassword = password
                },
                onFieldChanged = { clearedField = it },
            )
        }

        composeRule.onNodeWithText("用户名需为 3 至 32 位字母、数字或下划线").assertIsDisplayed()
        composeRule.onNodeWithTag(AuthTestTags.Username).performTextInput("listener_01")
        composeRule.onNodeWithTag(AuthTestTags.Password).performTextInput("correct-password")
        composeRule.onNodeWithTag(AuthTestTags.Password).performImeAction()

        assertThat(clearedField).isEqualTo(AuthField.Password)
        assertThat(submittedUsername).isEqualTo("listener_01")
        assertThat(submittedPassword).isEqualTo("correct-password")
    }

    @Test
    fun registrationCollectsUsernameAndPassword() {
        var submission: List<String>? = null
        composeRule.setAuthContent {
            RegisterScreen(
                uiState = AuthUiState(),
                onBack = {},
                onSubmit = { username, password, confirmation ->
                    submission = listOf(username, password, confirmation)
                },
                onFieldChanged = {},
            )
        }

        composeRule.onNodeWithTag(AuthTestTags.Username).performTextInput("listener_01")
        composeRule.onNodeWithTag(AuthTestTags.Password).performTextInput("password-123")
        composeRule.onNodeWithTag(AuthTestTags.ConfirmPassword).performTextInput("password-123")
        composeRule.onNodeWithTag(AuthTestTags.Submit).performScrollTo().performClick()

        assertThat(submission)
            .containsExactly(
                "listener_01",
                "password-123",
                "password-123",
            ).inOrder()
    }

    @Test
    fun submittingStateLocksSignInFormAndShowsProgressLabel() {
        val context = ApplicationProvider.getApplicationContext<Application>()
        composeRule.setAuthContent {
            SignInScreen(
                uiState = AuthUiState(isSubmitting = true),
                onBack = {},
                onSubmit = { _, _ -> },
                onFieldChanged = {},
            )
        }

        composeRule.onNodeWithTag(AuthTestTags.Username).assertIsNotEnabled()
        composeRule.onNodeWithTag(AuthTestTags.Submit).assertIsNotEnabled()
        composeRule
            .onNodeWithText(context.getString(R.string.auth_submitting))
            .performScrollTo()
            .assertIsDisplayed()
    }

    @Test
    @Config(qualifiers = "w900dp-h420dp-land")
    fun landscapeEntryPlacesBrandAndActionsSideBySide() {
        composeRule.setAuthContent {
            AuthEntryScreen(
                onSignIn = {},
                onRegister = {},
                serverAddress = "https://music.home:3000",
            )
        }

        val brandBounds =
            composeRule
                .onNodeWithTag(AuthTestTags.EntryBrand)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot
        val actionsBounds =
            composeRule
                .onNodeWithTag(AuthTestTags.EntryActions)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot

        assertThat(brandBounds.right).isLessThan(actionsBounds.left)
        composeRule.onNodeWithText("https://music.home:3000", substring = true).assertIsDisplayed()
    }

    @Test
    @Config(qualifiers = "w740dp-h320dp-land")
    fun compactLandscapeRegistrationKeepsScrollableFormActionsReachable() {
        composeRule.setAuthContent {
            RegisterScreen(
                uiState = AuthUiState(),
                onBack = {},
                onSubmit = { _, _, _ -> },
                onFieldChanged = {},
            )
        }

        val brandBounds =
            composeRule
                .onNodeWithTag(AuthTestTags.FormBrand)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot
        val formBounds =
            composeRule
                .onNodeWithTag(AuthTestTags.FormFields)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot

        assertThat(brandBounds.right).isLessThan(formBounds.left)
        composeRule.onNodeWithTag(AuthTestTags.ConfirmPassword).performScrollTo().assertIsDisplayed()
        composeRule.onNodeWithTag(AuthTestTags.Submit).performScrollTo().assertIsDisplayed()
    }

    private fun androidx.compose.ui.test.junit4.ComposeContentTestRule.setAuthContent(
        content: @androidx.compose.runtime.Composable () -> Unit,
    ) {
        setContent {
            XyMusicTheme(darkTheme = false, content = content)
        }
    }
}
