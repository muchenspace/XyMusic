package com.xymusic.app.feature.settings.domain

import com.xymusic.app.core.preferences.AppSettings
import com.xymusic.app.core.preferences.AppSettingsRepository
import com.xymusic.app.feature.settings.domain.model.AvatarUploadCommand
import com.xymusic.app.feature.settings.domain.model.UpdateProfileCommand
import javax.inject.Inject

class ProfileUseCases
@Inject
constructor(private val repository: ProfileRepository) {
    val profile = repository.profile

    suspend fun ensureLoaded() = repository.ensureLoaded()

    suspend fun refresh() = repository.refresh()

    suspend fun update(command: UpdateProfileCommand) = repository.update(command)

    suspend fun uploadAvatar(command: AvatarUploadCommand) = repository.uploadAvatar(command)

    suspend fun logoutAllSessions() = repository.logoutAllSessions()
}

class AppSettingsUseCases
@Inject
constructor(private val repository: AppSettingsRepository) {
    val settings = repository.settings

    suspend fun update(settings: AppSettings) = repository.update(settings)

    suspend fun mutate(transform: (AppSettings) -> AppSettings) = repository.mutate(transform)

    suspend fun reset() = repository.reset()
}
