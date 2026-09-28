package com.xymusic.app.core.ui.dns

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsOff
import androidx.compose.ui.test.assertIsOn
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTextClearance
import androidx.compose.ui.test.performTextInput
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.domain.settings.DnsSettings
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMusicTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ComposeTestApplication::class)
class DnsSettingsDialogTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun dialogDisplaysDefaultAliyunDnsSettings() {
        composeRule.setContent {
            XyMusicTheme {
                DnsSettingsDialog(
                    currentSettings = DnsSettings(),
                    onDismiss = {},
                    onSave = {},
                )
            }
        }

        composeRule.onNodeWithTag(DnsSettingsTestTags.Dialog).assertIsDisplayed()
        composeRule.onNodeWithTag(DnsSettingsTestTags.CustomDnsSwitch).assertIsOn()
        composeRule.onNodeWithTag(DnsSettingsTestTags.DnsServerInput).assertIsDisplayed()
        composeRule.onNodeWithTag(DnsSettingsTestTags.DohSwitch).performScrollTo().assertIsOn()
        composeRule.onNodeWithTag(DnsSettingsTestTags.DohUrlInput).performScrollTo().assertIsDisplayed()
        composeRule.onNodeWithTag(DnsSettingsTestTags.DisableCacheSwitch).performScrollTo().assertIsOff()
    }

    @Test
    fun savingSettingsEmitsUpdatedDnsSettings() {
        var saved: DnsSettings? = null
        composeRule.setContent {
            XyMusicTheme {
                DnsSettingsDialog(
                    currentSettings = DnsSettings(),
                    onDismiss = {},
                    onSave = { saved = it },
                )
            }
        }

        composeRule.onNodeWithTag(DnsSettingsTestTags.DnsServerInput).performTextClearance()
        composeRule.onNodeWithTag(DnsSettingsTestTags.DnsServerInput).performTextInput("1.1.1.1")

        composeRule.onNodeWithTag(DnsSettingsTestTags.DohUrlInput).performScrollTo().performTextClearance()
        composeRule.onNodeWithTag(DnsSettingsTestTags.DohUrlInput).performTextInput("https://1.1.1.1/dns-query")

        composeRule.onNodeWithTag(DnsSettingsTestTags.DisableCacheSwitch).performScrollTo().performClick()

        composeRule.onNodeWithTag(DnsSettingsTestTags.SaveButton).performClick()

        assertThat(saved).isNotNull()
        assertThat(saved?.customDnsEnabled).isTrue()
        assertThat(saved?.dnsServer).isEqualTo("1.1.1.1")
        assertThat(saved?.dohEnabled).isTrue()
        assertThat(saved?.dohUrl).isEqualTo("https://1.1.1.1/dns-query")
        assertThat(saved?.disableDnsCache).isTrue()
    }

    @Test
    fun dismissButtonTriggersOnDismiss() {
        var dismissed = false
        composeRule.setContent {
            XyMusicTheme {
                DnsSettingsDialog(
                    currentSettings = DnsSettings(),
                    onDismiss = { dismissed = true },
                    onSave = {},
                )
            }
        }

        composeRule.onNodeWithTag(DnsSettingsTestTags.CancelButton).performClick()
        assertThat(dismissed).isTrue()
    }

    @Test
    fun resetButtonRestoresAliyunDefaults() {
        var saved: DnsSettings? = null
        val customInitial = DnsSettings(
            customDnsEnabled = false,
            dnsServer = "8.8.8.8",
            dohEnabled = false,
            dohUrl = "https://dns.google/dns-query",
            disableDnsCache = true,
        )
        composeRule.setContent {
            XyMusicTheme {
                DnsSettingsDialog(
                    currentSettings = customInitial,
                    onDismiss = {},
                    onSave = { saved = it },
                )
            }
        }

        composeRule.onNodeWithTag(DnsSettingsTestTags.ResetButton).performScrollTo().performClick()
        composeRule.onNodeWithTag(DnsSettingsTestTags.SaveButton).performClick()

        assertThat(saved).isEqualTo(DnsSettings())
    }
}
