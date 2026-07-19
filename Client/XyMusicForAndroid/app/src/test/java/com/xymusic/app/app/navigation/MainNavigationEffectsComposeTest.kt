package com.xymusic.app.app.navigation

import androidx.compose.foundation.layout.Box
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.remember
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.R
import com.xymusic.app.feature.player.presentation.PlayerUiEffect
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMusicTheme
import kotlinx.coroutines.flow.MutableSharedFlow
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ComposeTestApplication::class)
class MainNavigationEffectsComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun playerEffectIsRenderedInTheSharedSnackbarHost() {
        val effects = MutableSharedFlow<PlayerUiEffect>(extraBufferCapacity = 1)
        composeRule.setContent {
            XyMusicTheme(dynamicColor = false) {
                val snackbarHostState = remember { SnackbarHostState() }
                Box {
                    SnackbarHost(snackbarHostState)
                    PlayerEffectSnackbar(effects, snackbarHostState)
                }
            }
        }
        composeRule.waitForIdle()

        composeRule.runOnIdle {
            assertThat(
                effects.tryEmit(
                    PlayerUiEffect.ShowMessage(R.string.player_codec_fallback_applied),
                ),
            ).isTrue()
        }

        composeRule
            .onNodeWithText(
                RuntimeEnvironment
                    .getApplication()
                    .getString(R.string.player_codec_fallback_applied),
            ).assertIsDisplayed()
    }
}
