package com.xymusic.app.app.navigation

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertHeightIsEqualTo
import androidx.compose.ui.test.assertWidthIsEqualTo
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMusicTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(
    sdk = [34],
    application = ComposeTestApplication::class,
    qualifiers = "w740dp-h320dp-land",
)
class MainNavigationChromeComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun navigationRailFillsCompactLandscapeHeight() {
        composeRule.setContent {
            XyMusicTheme(dynamicColor = false) {
                Box(modifier = Modifier.size(width = 200.dp, height = 240.dp)) {
                    MainNavigationRail(
                        currentDestination = MainDestination.Home,
                        onDestinationSelected = {},
                        modifier = Modifier.testTag(NAVIGATION_RAIL_TAG),
                    )
                }
            }
        }

        composeRule
            .onNodeWithTag(NAVIGATION_RAIL_TAG)
            .assertWidthIsEqualTo(72.dp)
            .assertHeightIsEqualTo(240.dp)
    }

    private companion object {
        const val NAVIGATION_RAIL_TAG = "main_navigation_rail"
    }
}
