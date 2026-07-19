package com.xymusic.app.feature.auth.presentation

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.network.model.ProblemCode
import com.xymusic.app.feature.auth.domain.AuthResult
import com.xymusic.app.feature.auth.domain.AuthUseCases
import com.xymusic.app.feature.auth.domain.model.LoginCommand
import com.xymusic.app.feature.auth.domain.model.RegisterCommand
import dagger.hilt.android.lifecycle.HiltViewModel
import java.util.concurrent.CancellationException
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

@HiltViewModel
class AuthViewModel
@Inject
constructor(private val useCases: AuthUseCases) : ViewModel() {
    private val mutableUiState = MutableStateFlow(AuthUiState())
    val uiState: StateFlow<AuthUiState> = mutableUiState.asStateFlow()

    private val mutableEffects = MutableSharedFlow<AuthEffect>(extraBufferCapacity = 1)
    val effects: SharedFlow<AuthEffect> = mutableEffects.asSharedFlow()

    fun register(username: String, password: String, confirmPassword: String) {
        val command =
            RegisterCommand(
                username = username.trim(),
                password = password,
            )
        val errors = validateRegistration(command, confirmPassword)
        if (!beginSubmission(errors)) return

        launchSubmission {
            when (val result = useCases.register(command)) {
                is AuthResult.Success ->
                    mutableEffects.emit(
                        AuthEffect.NavigateToSignIn(AuthMessage.AccountCreated),
                    )
                is AuthResult.Failure -> handleFailure(result.error)
            }
        }
    }

    fun login(username: String, password: String) {
        val command = LoginCommand(username.trim(), password)
        val errors = validateLogin(command)
        if (!beginSubmission(errors)) return

        launchSubmission {
            when (val result = useCases.login(command)) {
                is AuthResult.Success -> Unit
                is AuthResult.Failure -> handleFailure(result.error)
            }
        }
    }

    fun logout() {
        if (!beginSubmission(emptyMap())) return
        launchSubmission {
            when (val result = useCases.logout()) {
                is AuthResult.Success -> Unit
                is AuthResult.Failure -> handleFailure(result.error)
            }
        }
    }

    fun clearFieldError(field: AuthField) {
        mutableUiState.update { state ->
            state.copy(fieldErrors = state.fieldErrors - field)
        }
    }

    private fun beginSubmission(errors: Map<AuthField, AuthFieldError>): Boolean {
        if (mutableUiState.value.isSubmitting) return false
        mutableUiState.update { it.copy(fieldErrors = errors) }
        if (errors.isNotEmpty()) return false
        mutableUiState.update { it.copy(isSubmitting = true) }
        return true
    }

    private fun endSubmission() {
        mutableUiState.update { it.copy(isSubmitting = false) }
    }

    private fun launchSubmission(action: suspend () -> Unit) {
        viewModelScope.launch {
            try {
                action()
            } catch (failure: CancellationException) {
                throw failure
            } catch (_: Exception) {
                mutableEffects.emit(AuthEffect.ShowMessage(AuthMessage.GenericError))
            } finally {
                endSubmission()
            }
        }
    }

    private suspend fun handleFailure(error: DomainError) {
        val fieldErrors = error.toFieldErrors()
        if (fieldErrors.isNotEmpty()) {
            mutableUiState.update { it.copy(fieldErrors = fieldErrors) }
            return
        }
        mutableEffects.emit(AuthEffect.ShowMessage(error.toMessage()))
    }

    private fun DomainError.toFieldErrors(): Map<AuthField, AuthFieldError> = when (this) {
        is DomainError.Validation -> {
            val serverErrors =
                fieldErrors
                    .mapNotNull { (name, messages) ->
                        fieldForWireName(name)?.let { field ->
                            field to AuthFieldError.Server(messages.firstOrNull().orEmpty())
                        }
                    }.toMap()
            serverErrors
        }
        is DomainError.Conflict ->
            when (reason) {
                ProblemCode.DuplicateUsername -> mapOf(AuthField.Username to AuthFieldError.UsernameTaken)
                else -> emptyMap()
            }
        else -> emptyMap()
    }

    private fun DomainError.toMessage(): AuthMessage = when (this) {
        is DomainError.Authentication ->
            when (reason) {
                ProblemCode.InvalidCredentials -> AuthMessage.InvalidCredentials
                else -> AuthMessage.GenericError
            }
        is DomainError.PermissionDenied ->
            when (reason) {
                ProblemCode.AccountSuspended -> AuthMessage.AccountSuspended
                else -> detailedServerMessage()
            }
        is DomainError.Network -> AuthMessage.NetworkFailure(reason)
        is DomainError.ServiceUnavailable -> AuthMessage.ServerUnavailable
        is DomainError.RateLimited -> AuthMessage.RateLimited(retryAfterSeconds)
        is DomainError.Validation -> AuthMessage.InvalidInput
        is DomainError.Conflict -> detailedServerMessage()
        is DomainError.Server -> detailedServerMessage()
        is DomainError.NotFound -> AuthMessage.LoginEndpointNotFound
        is DomainError.Protocol -> AuthMessage.ProtocolError
        is DomainError.Local -> AuthMessage.LocalDataError
    }

    private fun DomainError.detailedServerMessage(): AuthMessage.Server {
        val message = buildString {
            append(detail.ifBlank { "请求未完成" })
            traceId?.trim()?.takeIf(String::isNotEmpty)?.let { append("（追踪 ID：$it）") }
        }
        return AuthMessage.Server(message)
    }

    private fun validateRegistration(
        command: RegisterCommand,
        confirmPassword: String,
    ): Map<AuthField, AuthFieldError> = buildMap {
        if (!USERNAME_REGEX.matches(command.username)) {
            put(AuthField.Username, AuthFieldError.InvalidUsername)
        }
        if (!isPasswordValid(command.password)) {
            put(AuthField.Password, AuthFieldError.InvalidPassword)
        }
        if (command.password != confirmPassword) {
            put(AuthField.ConfirmPassword, AuthFieldError.PasswordMismatch)
        }
    }

    private fun validateLogin(command: LoginCommand): Map<AuthField, AuthFieldError> = buildMap {
        if (!USERNAME_REGEX.matches(command.username)) {
            put(AuthField.Username, AuthFieldError.InvalidUsername)
        }
        if (command.password.isEmpty() || command.password.length > MAX_PASSWORD_LENGTH) {
            put(AuthField.Password, AuthFieldError.Required)
        }
    }

    private fun fieldForWireName(name: String): AuthField? = when (name) {
        "username" -> AuthField.Username
        "displayName" -> AuthField.DisplayName
        "password" -> AuthField.Password
        else -> null
    }

    private fun isPasswordValid(value: String): Boolean = value.length in MIN_PASSWORD_LENGTH..MAX_PASSWORD_LENGTH

    private companion object {
        val USERNAME_REGEX = Regex("^[A-Za-z0-9_]{3,32}$")
        const val MIN_PASSWORD_LENGTH = 8
        const val MAX_PASSWORD_LENGTH = 128
    }
}
