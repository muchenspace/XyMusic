package com.xymusic.app.feature.auth.domain

import com.xymusic.app.feature.auth.domain.model.LoginCommand
import com.xymusic.app.feature.auth.domain.model.RegisterCommand
import javax.inject.Inject

class AuthUseCases
@Inject
constructor(private val repository: AuthRepository) {
    suspend fun register(command: RegisterCommand) = repository.register(command)

    suspend fun login(command: LoginCommand) = repository.login(command)

    suspend fun logout() = repository.logout()
}
