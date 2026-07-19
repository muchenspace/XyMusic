package com.xymusic.app.feature.settings.presentation

import android.content.Context
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.hasScrollToIndexAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.ComposeContentTestRule
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToIndex
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.R
import com.xymusic.app.core.network.ServerEndpoint
import com.xymusic.app.core.preferences.AppSettings
import com.xymusic.app.core.preferences.AppSettingsRepository
import com.xymusic.app.core.preferences.MobileDataPolicy
import com.xymusic.app.core.preferences.ThemePreference
import com.xymusic.app.feature.auth.domain.AuthRepository
import com.xymusic.app.feature.auth.domain.AuthResult
import com.xymusic.app.feature.auth.domain.AuthUseCases
import com.xymusic.app.feature.auth.domain.model.LoginCommand
import com.xymusic.app.feature.auth.domain.model.RegisterCommand
import com.xymusic.app.feature.settings.domain.AppSettingsUseCases
import com.xymusic.app.feature.settings.domain.ProfileRepository
import com.xymusic.app.feature.settings.domain.ProfileUseCases
import com.xymusic.app.feature.settings.domain.SettingsResult
import com.xymusic.app.feature.settings.domain.model.AvatarUploadCommand
import com.xymusic.app.feature.settings.domain.model.UpdateProfileCommand
import com.xymusic.app.feature.settings.domain.model.UserProfile
import com.xymusic.app.feature.settings.domain.model.UserRole
import com.xymusic.app.feature.settings.domain.model.UserStatus
import com.xymusic.app.testing.ComposeTestApplication
import com.xymusic.app.ui.theme.XyMusicTheme
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ComposeTestApplication::class)
class SettingsScreenComposeTest {
    @get:Rule
    val composeRule = createComposeRule()

    private val context: Context
        get() = ApplicationProvider.getApplicationContext()

    @Test
    fun rootAndDetailBackActionsAreExposed() {
        var backCount = 0
        var dynamicColorValue: Boolean? = null
        composeRule.setSettingsContent(
            onBack = { backCount += 1 },
            onDynamicColorChanged = { dynamicColorValue = it },
        )

        composeRule.waitForIdle()
        composeRule.onNodeWithTag(SettingsTestTags.Root).assertIsDisplayed()
        SettingsPage.entries.forEach { page ->
            composeRule.onNodeWithText(context.getString(page.titleRes)).assertIsDisplayed()
        }
        composeRule.onNodeWithContentDescription(context.getString(R.string.common_back)).performClick()
        composeRule.onNodeWithText(context.getString(R.string.settings_appearance)).performClick()
        composeRule
            .onNodeWithText(context.getString(R.string.settings_dynamic_color))
            .performClick()
        composeRule.onNodeWithContentDescription(context.getString(R.string.common_back)).performClick()

        assertThat(backCount).isEqualTo(1)
        assertThat(dynamicColorValue).isTrue()
        composeRule.onNodeWithTag(SettingsTestTags.Root).assertIsDisplayed()
    }

    @Test
    fun wifiOnlyRowPersistsTheSelectedPolicy() {
        val settingsRepository = InMemoryAppSettingsRepository()
        composeRule.setSettingsContent(settingsRepository = settingsRepository)

        composeRule.waitForIdle()
        composeRule.onNodeWithText(context.getString(R.string.settings_playback)).performClick()
        composeRule
            .onNodeWithText(context.getString(R.string.settings_wifi_only))
            .performClick()
        composeRule.waitForIdle()

        assertThat(settingsRepository.current.mobileDataPolicy).isEqualTo(MobileDataPolicy.WIFI_ONLY)
    }

    @Test
    fun themeOptionsUseRequestedOrderAndSelectColorTheme() {
        assertThat(ThemePreference.entries)
            .containsExactly(
                ThemePreference.SYSTEM,
                ThemePreference.LIGHT,
                ThemePreference.DARK,
                ThemePreference.PEACH_PINK,
                ThemePreference.OCEAN_BLUE,
                ThemePreference.TWILIGHT_PURPLE,
            ).inOrder()
        val settingsRepository = InMemoryAppSettingsRepository()
        composeRule.setSettingsContent(settingsRepository = settingsRepository)

        composeRule.waitForIdle()
        composeRule.onNodeWithText(context.getString(R.string.settings_appearance)).performClick()
        composeRule
            .onNode(
                hasText(context.getString(R.string.settings_theme_system)) and hasClickAction(),
            ).performClick()
        composeRule.onNodeWithText(context.getString(R.string.settings_theme_peach)).assertIsDisplayed()
        composeRule.onNodeWithText(context.getString(R.string.settings_theme_twilight)).assertIsDisplayed()
        composeRule.onNodeWithText(context.getString(R.string.settings_theme_ocean)).performClick()
        composeRule.waitForIdle()

        assertThat(settingsRepository.current.theme).isEqualTo(ThemePreference.OCEAN_BLUE)
    }

    @Test
    fun fixedColorThemeDisablesSystemDynamicColor() {
        val settingsRepository =
            InMemoryAppSettingsRepository(
                AppSettings(theme = ThemePreference.PEACH_PINK),
            )
        composeRule.setSettingsContent(settingsRepository = settingsRepository)

        composeRule.waitForIdle()
        composeRule.onNodeWithText(context.getString(R.string.settings_appearance)).performClick()
        composeRule
            .onNodeWithText(context.getString(R.string.settings_dynamic_color))
            .assertIsNotEnabled()
        composeRule
            .onNodeWithText(
                context.getString(R.string.settings_dynamic_color_fixed_theme_summary),
            ).assertIsDisplayed()
    }

    @Test
    fun wordByWordLyricsRowPersistsTheSelectedValue() {
        val settingsRepository = InMemoryAppSettingsRepository()
        composeRule.setSettingsContent(settingsRepository = settingsRepository)

        composeRule.waitForIdle()
        composeRule.onNodeWithText(context.getString(R.string.settings_playback)).performClick()
        composeRule.onNode(hasScrollToIndexAction()).performScrollToIndex(3)
        composeRule
            .onNodeWithText(context.getString(R.string.settings_word_by_word_lyrics))
            .performClick()
        composeRule.waitForIdle()

        assertThat(settingsRepository.current.wordByWordLyricsEnabled).isFalse()
    }

    @Test
    fun profileAndServerDetailsKeepTheirExistingActions() {
        composeRule.setSettingsContent()

        composeRule.waitForIdle()
        composeRule.onNodeWithText(context.getString(R.string.settings_profile)).performClick()
        composeRule.onNodeWithText("Lin Chen").assertIsDisplayed()
        composeRule
            .onNodeWithContentDescription(context.getString(R.string.settings_edit_profile))
            .performClick()
        composeRule.onNodeWithTag(SettingsDialogTestTags.DisplayName).assertIsDisplayed()
        composeRule.onNodeWithText(context.getString(R.string.common_cancel)).performClick()

        composeRule.onNodeWithContentDescription(context.getString(R.string.common_back)).performClick()
        composeRule.onNodeWithText(context.getString(R.string.settings_server)).performClick()
        composeRule.onNodeWithText("https://localhost:3000").assertIsDisplayed()
    }

    @Test
    @Config(qualifiers = "w740dp-h320dp-land")
    fun compactLandscapeSettingsShowsIndependentGroupedColumns() {
        composeRule.setSettingsContent()

        composeRule.waitForIdle()
        val leftBounds =
            composeRule
                .onNodeWithTag(SettingsTestTags.LandscapeLeft)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot
        val rightBounds =
            composeRule
                .onNodeWithTag(SettingsTestTags.LandscapeRight)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot

        assertThat(leftBounds.right).isLessThan(rightBounds.left)
        composeRule.onNodeWithText("Lin Chen").assertIsDisplayed()
        composeRule.onNodeWithText(context.getString(R.string.settings_playback)).assertIsDisplayed()
        composeRule.onNodeWithTag(SettingsTestTags.page(SettingsPage.Playback)).performClick()
        composeRule.onNodeWithTag(SettingsTestTags.LandscapeRight).performScrollToIndex(4)
        composeRule
            .onNodeWithText(context.getString(R.string.settings_word_by_word_lyrics))
            .assertIsDisplayed()
    }

    @Test
    @Config(qualifiers = "w740dp-h320dp-land")
    fun compactLandscapeProfileDialogKeepsBothFieldsAndConfirmVisible() {
        composeRule.setContent {
            XyMusicTheme(darkTheme = false) {
                EditProfileDialog(
                    profile = profile(),
                    onDismiss = {},
                    onSave = { _, _ -> },
                )
            }
        }

        val displayNameBounds =
            composeRule
                .onNodeWithTag(SettingsDialogTestTags.DisplayName)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot
        val bioBounds =
            composeRule
                .onNodeWithTag(SettingsDialogTestTags.Bio)
                .assertIsDisplayed()
                .fetchSemanticsNode()
                .boundsInRoot

        assertThat(displayNameBounds.right).isLessThan(bioBounds.left)
        composeRule.onNodeWithTag(SettingsDialogTestTags.Confirm).assertIsDisplayed()
    }

    private fun ComposeContentTestRule.setSettingsContent(
        settingsRepository: InMemoryAppSettingsRepository = InMemoryAppSettingsRepository(),
        onBack: () -> Unit = {},
        onDynamicColorChanged: (Boolean) -> Unit = {},
    ) {
        val viewModel =
            SettingsViewModel(
                profileUseCases = ProfileUseCases(FakeProfileRepository(profile())),
                appSettingsUseCases = AppSettingsUseCases(settingsRepository),
                authUseCases = AuthUseCases(SuccessAuthRepository()),
                ioDispatcher = Dispatchers.Unconfined,
                avatarImageNormalizer = AvatarImageNormalizer(context.applicationContext),
            )
        setContent {
            XyMusicTheme(darkTheme = false) {
                SettingsScreen(
                    dynamicColorEnabled = false,
                    onDynamicColorChanged = onDynamicColorChanged,
                    serverEndpoint = checkNotNull(ServerEndpoint.parse("localhost", "3000")),
                    onServerEndpointChanged = {},
                    onBack = onBack,
                    viewModel = viewModel,
                )
            }
        }
    }

    private fun profile() = UserProfile(
        id = "user-1",
        username = "linchen",
        displayName = "Lin Chen",
        bio = "Listening across devices",
        avatar = null,
        role = UserRole.USER,
        status = UserStatus.ACTIVE,
        version = 1,
        createdAtEpochMillis = 1_000,
        updatedAtEpochMillis = 1_000,
    )
}

private class FakeProfileRepository(initialProfile: UserProfile) : ProfileRepository {
    private val profileFlow = MutableStateFlow<UserProfile?>(initialProfile)
    override val profile: Flow<UserProfile?> = profileFlow

    override suspend fun refresh(): SettingsResult<UserProfile> =
        SettingsResult.Success(checkNotNull(profileFlow.value))

    override suspend fun update(command: UpdateProfileCommand): SettingsResult<UserProfile> =
        SettingsResult.Success(checkNotNull(profileFlow.value))

    override suspend fun uploadAvatar(command: AvatarUploadCommand): SettingsResult<UserProfile> =
        SettingsResult.Success(checkNotNull(profileFlow.value))

    override suspend fun logoutAllSessions(): SettingsResult<Unit> = SettingsResult.Success(Unit)
}

private class InMemoryAppSettingsRepository(initialSettings: AppSettings = AppSettings()) : AppSettingsRepository {
    private val settingsFlow = MutableStateFlow(initialSettings)
    override val settings: Flow<AppSettings> = settingsFlow
    val current: AppSettings
        get() = settingsFlow.value

    override suspend fun update(settings: AppSettings) {
        settingsFlow.value = settings
    }

    override suspend fun mutate(transform: (AppSettings) -> AppSettings) {
        settingsFlow.value = transform(settingsFlow.value)
    }

    override suspend fun reset() {
        settingsFlow.value = AppSettings()
    }
}

private class SuccessAuthRepository : AuthRepository {
    override suspend fun register(command: RegisterCommand) = error("Not used")

    override suspend fun login(command: LoginCommand) = error("Not used")

    override suspend fun logout(): AuthResult<Unit> = AuthResult.Success(Unit)
}
