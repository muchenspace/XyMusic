package com.xymusic.app.app.home

import app.cash.turbine.test
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.model.media.Artwork
import com.xymusic.app.feature.settings.domain.ProfileRepository
import com.xymusic.app.feature.settings.domain.ProfileUseCases
import com.xymusic.app.feature.settings.domain.SettingsResult
import com.xymusic.app.feature.settings.domain.model.AvatarUploadCommand
import com.xymusic.app.feature.settings.domain.model.UpdateProfileCommand
import com.xymusic.app.feature.settings.domain.model.UserProfile
import com.xymusic.app.feature.settings.domain.model.UserRole
import com.xymusic.app.feature.settings.domain.model.UserStatus
import com.xymusic.app.support.MainDispatcherRule
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Rule
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class HomeProfileViewModelTest {
    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    @Test
    fun loadsProfileOnceAndExposesAvatarIdentity() = runTest {
        val repository = FakeProfileRepository()
        val viewModel = HomeProfileViewModel(ProfileUseCases(repository))

        viewModel.uiState.test {
            assertThat(awaitItem()).isEqualTo(HomeProfileUiState())
            advanceUntilIdle()

            assertThat(awaitItem()).isEqualTo(
                HomeProfileUiState(
                    avatarUrl = AVATAR.url,
                    avatarCacheKey = AVATAR.cacheKey,
                ),
            )
            assertThat(repository.ensureLoadedCalls).isEqualTo(1)
            cancelAndIgnoreRemainingEvents()
        }
    }

    private class FakeProfileRepository : ProfileRepository {
        private val mutableProfile = MutableStateFlow<UserProfile?>(null)
        override val profile: Flow<UserProfile?> = mutableProfile
        var ensureLoadedCalls = 0

        override suspend fun ensureLoaded(): SettingsResult<UserProfile> {
            ensureLoadedCalls += 1
            mutableProfile.value = PROFILE
            return SettingsResult.Success(PROFILE)
        }

        override suspend fun refresh(): SettingsResult<UserProfile> = SettingsResult.Success(PROFILE)

        override suspend fun update(command: UpdateProfileCommand): SettingsResult<UserProfile> =
            SettingsResult.Success(PROFILE)

        override suspend fun uploadAvatar(command: AvatarUploadCommand): SettingsResult<UserProfile> =
            SettingsResult.Success(PROFILE)

        override suspend fun logoutAllSessions(): SettingsResult<Unit> = SettingsResult.Success(Unit)
    }

    private companion object {
        val AVATAR =
            Artwork(
                assetId = "avatar-1",
                url = "https://media.example/avatar.jpg",
                cacheKey = "avatar:user-1:v2",
                mimeType = "image/jpeg",
                expiresAtEpochMillis = null,
                width = 512,
                height = 512,
            )
        val PROFILE =
            UserProfile(
                id = "user-1",
                username = "alice",
                displayName = "Alice",
                bio = null,
                avatar = AVATAR,
                role = UserRole.USER,
                status = UserStatus.ACTIVE,
                version = 2,
                createdAtEpochMillis = 1_000,
                updatedAtEpochMillis = 2_000,
            )
    }
}
