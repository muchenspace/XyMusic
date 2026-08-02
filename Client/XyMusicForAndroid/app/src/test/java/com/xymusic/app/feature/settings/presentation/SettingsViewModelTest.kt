package com.xymusic.app.feature.settings.presentation

import android.app.Application
import app.cash.turbine.test
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.R
import com.xymusic.app.domain.error.DomainError
import com.xymusic.app.domain.settings.AppSettings
import com.xymusic.app.domain.settings.AppSettingsRepository
import com.xymusic.app.domain.settings.AppSettingsUseCases
import com.xymusic.app.feature.auth.domain.AuthRepository
import com.xymusic.app.feature.auth.domain.AuthResult
import com.xymusic.app.feature.auth.domain.AuthUseCases
import com.xymusic.app.feature.auth.domain.model.LoginCommand
import com.xymusic.app.feature.auth.domain.model.RegisterCommand
import com.xymusic.app.feature.settings.domain.AvatarImageException
import com.xymusic.app.feature.settings.domain.AvatarImageNormalizer
import com.xymusic.app.feature.settings.domain.AvatarImageSource
import com.xymusic.app.feature.settings.domain.AvatarImageTooLargeException
import com.xymusic.app.feature.settings.domain.InvalidAvatarImageException
import com.xymusic.app.feature.settings.domain.ProfileRepository
import com.xymusic.app.feature.settings.domain.ProfileUseCases
import com.xymusic.app.feature.settings.domain.SettingsResult
import com.xymusic.app.feature.settings.domain.model.AvatarUploadCommand
import com.xymusic.app.feature.settings.domain.model.UpdateProfileCommand
import com.xymusic.app.feature.settings.domain.model.UserProfile
import com.xymusic.app.feature.settings.domain.model.UserRole
import com.xymusic.app.feature.settings.domain.model.UserStatus
import com.xymusic.app.support.MainDispatcherRule
import java.io.ByteArrayInputStream
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class SettingsViewModelTest {
    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    @Test
    fun invalidAvatarShowsImageSpecificMessageWithoutCallingApi() = runTest {
        val repository = FakeProfileRepository()
        val viewModel =
            viewModel(
                repository = repository,
                normalizer = normalizer(InvalidAvatarImageException()),
            )

        viewModel.effects.test {
            viewModel.uploadAvatar(TEST_SOURCE)
            advanceUntilIdle()

            assertThat(awaitItem()).isEqualTo(
                SettingsUiEffect.ShowMessage(R.string.settings_avatar_invalid_image),
            )
            assertThat(repository.uploadCalls).isEqualTo(0)
        }
    }

    @Test
    fun avatarThatCannotFitLimitShowsTooLargeMessageWithoutCallingApi() = runTest {
        val repository = FakeProfileRepository()
        val viewModel =
            viewModel(
                repository = repository,
                normalizer = normalizer(AvatarImageTooLargeException()),
            )

        viewModel.effects.test {
            viewModel.uploadAvatar(TEST_SOURCE)
            advanceUntilIdle()

            assertThat(awaitItem()).isEqualTo(
                SettingsUiEffect.ShowMessage(R.string.settings_avatar_too_large),
            )
            assertThat(repository.uploadCalls).isEqualTo(0)
        }
    }

    @Test
    fun avatarApiFailureUsesUploadSpecificMessage() = runTest {
        val repository =
            FakeProfileRepository().apply {
                uploadResult = SettingsResult.Failure(DomainError.Network("offline"))
            }
        val viewModel = viewModel(repository, normalizer())

        viewModel.effects.test {
            viewModel.uploadAvatar(TEST_SOURCE)
            advanceUntilIdle()

            assertThat(awaitItem()).isEqualTo(
                SettingsUiEffect.ShowMessage(R.string.settings_avatar_upload_failed),
            )
            assertThat(repository.uploadCalls).isEqualTo(1)
            assertThat(repository.lastUpload?.contentType).isEqualTo("image/jpeg")
        }
    }

    @Test
    fun unexpectedProfileRefreshFailureClearsLoadingAndShowsChineseMessage() = runTest {
        val repository =
            FakeProfileRepository().apply {
                refreshFailure = IllegalStateException("database implementation detail")
            }
        val viewModel = viewModel(repository, normalizer())

        viewModel.uiState.test {
            awaitItem()
            viewModel.effects.test {
                advanceUntilIdle()

                assertThat(awaitItem()).isEqualTo(
                    SettingsUiEffect.ShowMessage(R.string.settings_profile_refresh_failed),
                )
                assertThat(viewModel.uiState.value.isRefreshingProfile).isFalse()
            }
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun localLogoutFailureAfterGlobalRevocationIsReportedAndClearsBusyState() = runTest {
        val authRepository =
            FakeAuthRepository().apply {
                logoutResult = AuthResult.Failure(DomainError.Network("offline"))
            }
        val viewModel =
            viewModel(
                repository = FakeProfileRepository(),
                normalizer = normalizer(),
                authRepository = authRepository,
            )
        advanceUntilIdle()

        viewModel.uiState.test {
            awaitItem()
            viewModel.effects.test {
                viewModel.logoutAllSessions()
                advanceUntilIdle()

                assertThat(awaitItem()).isEqualTo(
                    SettingsUiEffect.ShowMessage(R.string.settings_logout_failed),
                )
                assertThat(viewModel.uiState.value.isSaving).isFalse()
            }
            cancelAndIgnoreRemainingEvents()
        }
    }

    private fun viewModel(
        repository: FakeProfileRepository,
        normalizer: AvatarImageNormalizer,
        authRepository: FakeAuthRepository = FakeAuthRepository(),
        appSettingsRepository: FakeAppSettingsRepository = FakeAppSettingsRepository(),
    ) = SettingsViewModel(
        profileUseCases = ProfileUseCases(repository),
        appSettingsUseCases = AppSettingsUseCases(appSettingsRepository),
        authUseCases = AuthUseCases(authRepository),
        ioDispatcher = mainDispatcherRule.dispatcher,
        avatarImageNormalizer = normalizer,
    )

    private fun normalizer(failure: AvatarImageException? = null): AvatarImageNormalizer =
        object : AvatarImageNormalizer {
            override fun normalize(source: AvatarImageSource): AvatarUploadCommand {
                failure?.let { throw it }
                return AvatarUploadCommand(
                    fileName = "avatar.jpg",
                    contentType = "image/jpeg",
                    content = byteArrayOf(1),
                )
            }
        }

    private class FakeProfileRepository : ProfileRepository {
        private val currentProfile = profile()
        override val profile: Flow<UserProfile?> = MutableStateFlow(currentProfile)
        var uploadCalls = 0
        var lastUpload: AvatarUploadCommand? = null
        var uploadResult: SettingsResult<UserProfile> = SettingsResult.Success(currentProfile)
        var refreshFailure: Throwable? = null

        override suspend fun refresh(): SettingsResult<UserProfile> {
            refreshFailure?.let { throw it }
            return SettingsResult.Success(currentProfile)
        }

        override suspend fun update(command: UpdateProfileCommand): SettingsResult<UserProfile> =
            SettingsResult.Success(currentProfile)

        override suspend fun uploadAvatar(command: AvatarUploadCommand): SettingsResult<UserProfile> {
            uploadCalls += 1
            lastUpload = command
            return uploadResult
        }

        override suspend fun logoutAllSessions(): SettingsResult<Unit> = SettingsResult.Success(Unit)
    }

    private class FakeAppSettingsRepository(initialSettings: AppSettings = AppSettings()) :
        AppSettingsRepository {
        private val state = MutableStateFlow(initialSettings)
        override val settings: Flow<AppSettings> = state

        override suspend fun update(settings: AppSettings) {
            state.value = settings
        }

        override suspend fun mutate(transform: (AppSettings) -> AppSettings) {
            state.value = transform(state.value)
        }

        override suspend fun reset() {
            state.value = AppSettings()
        }
    }

    private class FakeAuthRepository : AuthRepository {
        var logoutResult: AuthResult<Unit> = AuthResult.Success(Unit)

        override suspend fun register(command: RegisterCommand) = error("Not used")

        override suspend fun login(command: LoginCommand) = error("Not used")

        override suspend fun logout(): AuthResult<Unit> = logoutResult
    }

    private companion object {
        val TEST_SOURCE = AvatarImageSource { ByteArrayInputStream(byteArrayOf(1)) }

        fun profile() = UserProfile(
            id = "user-1",
            username = "alice",
            displayName = "Alice",
            bio = null,
            avatar = null,
            role = UserRole.USER,
            status = UserStatus.ACTIVE,
            version = 1,
            createdAtEpochMillis = 1_000,
            updatedAtEpochMillis = 1_000,
        )
    }
}
