package com.xymusic.app.feature.settings.domain

import com.xymusic.app.feature.settings.domain.model.AvatarUploadCommand
import com.xymusic.app.feature.settings.domain.model.UpdateProfileCommand

class ProfileUseCases(private val repository: ProfileRepository) {
    val profile = repository.profile

    suspend fun ensureLoaded() = repository.ensureLoaded()

    suspend fun refresh() = repository.refresh()

    suspend fun update(command: UpdateProfileCommand) = repository.update(command)

    suspend fun uploadAvatar(command: AvatarUploadCommand) = repository.uploadAvatar(command)

    suspend fun logoutAllSessions() = repository.logoutAllSessions()
}
