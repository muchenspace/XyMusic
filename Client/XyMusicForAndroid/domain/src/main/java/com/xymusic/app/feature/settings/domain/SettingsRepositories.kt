package com.xymusic.app.feature.settings.domain

import com.xymusic.app.core.network.DomainError
import com.xymusic.app.feature.settings.domain.model.AvatarUploadCommand
import com.xymusic.app.feature.settings.domain.model.UpdateProfileCommand
import com.xymusic.app.feature.settings.domain.model.UserProfile
import kotlinx.coroutines.flow.Flow

interface ProfileRepository {
    val profile: Flow<UserProfile?>

    suspend fun ensureLoaded(): SettingsResult<UserProfile> = refresh()

    suspend fun refresh(): SettingsResult<UserProfile>

    suspend fun update(command: UpdateProfileCommand): SettingsResult<UserProfile>

    suspend fun uploadAvatar(command: AvatarUploadCommand): SettingsResult<UserProfile>

    suspend fun logoutAllSessions(): SettingsResult<Unit>
}

sealed interface SettingsResult<out T> {
    data class Success<T>(val value: T) : SettingsResult<T>

    data class Failure(val error: DomainError) : SettingsResult<Nothing>
}
