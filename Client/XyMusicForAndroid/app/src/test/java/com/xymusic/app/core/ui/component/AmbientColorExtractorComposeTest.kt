package com.xymusic.app.core.ui.component

import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.testing.ComposeTestApplication
import java.util.concurrent.atomic.AtomicReference
import org.junit.After
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ComposeTestApplication::class)
class AmbientColorExtractorComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Before
    fun setUp() {
        clearArtworkAmbientPaletteCacheForTest()
    }

    @After
    fun tearDown() {
        clearArtworkAmbientPaletteCacheForTest()
    }

    @Test
    fun cachedPaletteFollowsArtworkIdentityChanges() {
        val firstKey = "artwork:first:v1"
        val secondKey = "artwork:second:v1"
        val firstPalette =
            ArtworkAmbientPalette(
                primary = Color(0xFF112233),
                secondary = Color(0xFF223344),
                highlight = Color(0xFF334455),
            )
        val secondPalette =
            ArtworkAmbientPalette(
                primary = Color(0xFF665544),
                secondary = Color(0xFF776655),
                highlight = Color(0xFF887766),
            )
        cacheArtworkAmbientPaletteForTest(firstKey, firstPalette)
        cacheArtworkAmbientPaletteForTest(secondKey, secondPalette)

        lateinit var selectArtwork: (String) -> Unit
        val observedPalette = AtomicReference<ArtworkAmbientPalette?>()
        composeRule.setContent {
            var artworkKey by remember { mutableStateOf(firstKey) }
            selectArtwork = { artworkKey = it }
            observedPalette.set(
                rememberArtworkAmbientPalette(
                    artworkUrl = "https://media.example.test/$artworkKey.jpg",
                    cacheKey = artworkKey,
                ),
            )
            Text(artworkKey)
        }
        composeRule.waitForIdle()
        assertThat(observedPalette.get()).isEqualTo(firstPalette)

        composeRule.runOnIdle { selectArtwork(secondKey) }
        composeRule.onNodeWithText(secondKey).assertExists()
        composeRule.waitForIdle()

        assertThat(observedPalette.get()).isEqualTo(secondPalette)
    }
}
