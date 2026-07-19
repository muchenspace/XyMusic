package com.xymusic.app.feature.auth.domain

import com.xymusic.app.core.network.DomainError
import com.xymusic.app.feature.auth.domain.model.LoginCommand
import com.xymusic.app.feature.auth.domain.model.RegisterCommand
import com.xymusic.app.feature.auth.domain.model.RegistrationResult

interface AuthRepository {
    suspend fun register(command: RegisterCommand): AuthResult<RegistrationResult>

    suspend fun login(command: LoginCommand): AuthResult<Unit>

    suspend fun logout(): AuthResult<Unit>
}

sealed interface AuthResult<out T> {
    data class Success<T>(val value: T) : AuthResult<T>

    data class Failure(val error: DomainError) : AuthResult<Nothing>
}
