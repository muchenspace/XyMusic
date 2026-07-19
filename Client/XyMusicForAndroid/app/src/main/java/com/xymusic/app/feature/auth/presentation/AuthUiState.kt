package com.xymusic.app.feature.auth.presentation

import androidx.compose.runtime.Immutable
import com.xymusic.app.core.network.NetworkFailureReason

@Immutable
data class AuthUiState(val isSubmitting: Boolean = false, val fieldErrors: Map<AuthField, AuthFieldError> = emptyMap())

enum class AuthField {
    Username,
    DisplayName,
    Password,
    ConfirmPassword,
}

sealed interface AuthFieldError {
    data object Required : AuthFieldError

    data object InvalidUsername : AuthFieldError

    data object InvalidDisplayName : AuthFieldError

    data object InvalidPassword : AuthFieldError

    data object PasswordMismatch : AuthFieldError

    data object UsernameTaken : AuthFieldError

    data class Server(val message: String) : AuthFieldError
}

sealed interface AuthMessage {
    data object AccountCreated : AuthMessage

    data object InvalidCredentials : AuthMessage

    data object AccountSuspended : AuthMessage

    data class NetworkFailure(val reason: NetworkFailureReason) : AuthMessage

    data object ServerUnavailable : AuthMessage

    data object LoginEndpointNotFound : AuthMessage

    data object InvalidInput : AuthMessage

    data object ProtocolError : AuthMessage

    data object LocalDataError : AuthMessage

    data class RateLimited(val retryAfterSeconds: Long?) : AuthMessage

    data class Server(val message: String) : AuthMessage

    data object GenericError : AuthMessage
}

sealed interface AuthEffect {
    data class NavigateToSignIn(val message: AuthMessage? = null) : AuthEffect

    data class ShowMessage(val message: AuthMessage) : AuthEffect
}
