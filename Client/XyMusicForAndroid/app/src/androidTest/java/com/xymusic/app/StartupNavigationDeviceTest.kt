package com.xymusic.app

import android.content.Context
import android.os.ParcelFileDescriptor
import android.os.SystemClock
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTextInput
import androidx.test.core.app.ActivityScenario
import androidx.test.espresso.Espresso.pressBack
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import com.xymusic.app.core.ui.server.ServerConfigTestTags
import org.junit.After
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class StartupNavigationDeviceTest {
    @get:Rule
    val composeRule = createEmptyComposeRule()

    private lateinit var scenario: ActivityScenario<MainActivity>

    @Before
    fun setUp() {
        clearPersistedAuthentication()
        pauseMediaPlayback()
        scenario = ActivityScenario.launch(MainActivity::class.java)
    }

    @After
    fun tearDown() {
        scenario.close()
    }

    @Test
    fun startupAndAuthenticationNavigationRemainInteractive() {
        val resources = InstrumentationRegistry.getInstrumentation().targetContext.resources
        val setupTitle = resources.getString(R.string.server_setup_title)
        val signIn = resources.getString(R.string.auth_sign_in)
        val signInTitle = resources.getString(R.string.auth_sign_in_title)
        val createAccount = resources.getString(R.string.auth_create_account)
        val registerTitle = resources.getString(R.string.auth_register_title)

        composeRule.waitUntil(timeoutMillis = STARTUP_WAIT_MILLIS) {
            hasText(setupTitle) || hasText(signIn) || hasTag(MAIN_NAVIGATION_HOME)
        }
        if (hasTag(MAIN_NAVIGATION_HOME)) {
            composeRule.onNodeWithTag(MAIN_NAVIGATION_HOME, useUnmergedTree = true).assertExists()
            return
        }
        if (hasText(setupTitle)) {
            composeRule.onNodeWithTag(ServerConfigTestTags.Host).performTextInput("127.0.0.1")
            composeRule.onNodeWithTag(ServerConfigTestTags.Port).performTextInput("1")
            composeRule.onNodeWithTag(ServerConfigTestTags.Save).performClick()
            composeRule.waitUntil(timeoutMillis = STARTUP_WAIT_MILLIS) { hasText(signIn) }
        }

        composeRule.onNodeWithText(signIn, useUnmergedTree = true).performClick()
        composeRule.waitUntil(timeoutMillis = STARTUP_WAIT_MILLIS) { hasText(signInTitle) }
        composeRule.onNodeWithText(signInTitle, useUnmergedTree = true).assertExists()

        composeRule
            .onNodeWithText(createAccount, useUnmergedTree = true)
            .performScrollTo()
            .performClick()
        composeRule.waitUntil(timeoutMillis = STARTUP_WAIT_MILLIS) { hasText(registerTitle) }
        composeRule.onNodeWithText(registerTitle, useUnmergedTree = true).assertExists()

        pressBack()
        composeRule.waitUntil(timeoutMillis = STARTUP_WAIT_MILLIS) { hasText(signInTitle) }
        composeRule.onNodeWithText(signInTitle, useUnmergedTree = true).assertExists()

        pressBack()
        composeRule.waitUntil(timeoutMillis = STARTUP_WAIT_MILLIS) { hasText(signIn) }
        composeRule.onNodeWithText(createAccount, useUnmergedTree = true).performClick()
        composeRule.waitUntil(timeoutMillis = STARTUP_WAIT_MILLIS) { hasText(registerTitle) }
        composeRule.onNodeWithText(registerTitle, useUnmergedTree = true).assertExists()

        pressBack()
        composeRule.waitUntil(timeoutMillis = STARTUP_WAIT_MILLIS) { hasText(signIn) }
        composeRule.onNodeWithText(signIn, useUnmergedTree = true).assertExists()
    }

    private fun hasText(text: String): Boolean = composeRule
        .onAllNodesWithText(text, useUnmergedTree = true)
        .fetchSemanticsNodes()
        .isNotEmpty()

    private fun hasTag(tag: String): Boolean = composeRule
        .onAllNodesWithTag(tag, useUnmergedTree = true)
        .fetchSemanticsNodes()
        .isNotEmpty()

    private fun clearPersistedAuthentication() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        context
            .getSharedPreferences(SECURE_SESSION_PREFERENCES, Context.MODE_PRIVATE)
            .edit()
            .clear()
            .commit()
        context
            .getSharedPreferences(SERVER_CONFIG_PREFERENCES, Context.MODE_PRIVATE)
            .edit()
            .clear()
            .commit()
    }

    private fun pauseMediaPlayback() {
        val descriptor =
            InstrumentationRegistry.getInstrumentation()
                .uiAutomation
                .executeShellCommand("input keyevent KEYCODE_MEDIA_PAUSE")
        ParcelFileDescriptor.AutoCloseInputStream(descriptor).use { it.readBytes() }
        SystemClock.sleep(150L)
    }

    private companion object {
        const val STARTUP_WAIT_MILLIS = 15_000L
        const val SECURE_SESSION_PREFERENCES = "secure_session"
        const val SERVER_CONFIG_PREFERENCES = "xy_music_server_config"
        const val MAIN_NAVIGATION_HOME = "main_navigation_home"
    }
}
