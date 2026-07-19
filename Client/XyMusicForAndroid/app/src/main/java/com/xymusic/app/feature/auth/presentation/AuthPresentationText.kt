package com.xymusic.app.feature.auth.presentation

import android.content.Context
import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import com.xymusic.app.R
import com.xymusic.app.core.network.NetworkFailureReason

@Composable
internal fun authFieldErrorText(error: AuthFieldError): String = when (error) {
    AuthFieldError.Required -> stringResource(R.string.auth_error_required)
    AuthFieldError.InvalidUsername -> stringResource(R.string.auth_error_username)
    AuthFieldError.InvalidDisplayName -> stringResource(R.string.auth_error_display_name)
    AuthFieldError.InvalidPassword -> stringResource(R.string.auth_error_password)
    AuthFieldError.PasswordMismatch -> stringResource(R.string.auth_error_password_mismatch)
    AuthFieldError.UsernameTaken -> stringResource(R.string.auth_error_username_taken)
    is AuthFieldError.Server -> stringResource(R.string.auth_error_server_validation)
}

internal fun AuthMessage.resolve(context: Context): String = when (this) {
    AuthMessage.AccountCreated -> context.getString(R.string.auth_message_account_created)
    AuthMessage.InvalidCredentials -> context.getString(R.string.auth_message_invalid_credentials)
    AuthMessage.AccountSuspended -> context.getString(R.string.auth_message_account_suspended)
    is AuthMessage.NetworkFailure -> context.getString(reason.messageResource)
    AuthMessage.ServerUnavailable -> context.getString(R.string.auth_message_server_unavailable)
    AuthMessage.LoginEndpointNotFound -> context.getString(R.string.auth_message_login_endpoint_not_found)
    AuthMessage.InvalidInput -> context.getString(R.string.auth_message_invalid_input)
    AuthMessage.ProtocolError -> context.getString(R.string.auth_message_protocol_error)
    AuthMessage.LocalDataError -> context.getString(R.string.auth_message_local_data_error)
    is AuthMessage.RateLimited ->
        retryAfterSeconds?.let { seconds ->
            context.getString(R.string.auth_message_rate_limited_seconds, seconds)
        } ?: context.getString(R.string.auth_message_rate_limited)
    is AuthMessage.Server ->
        message.userFacingServerMessageOrNull()
            ?: context.getString(R.string.auth_message_server_error)
    AuthMessage.GenericError -> context.getString(R.string.common_error_message)
}

@get:StringRes
private val NetworkFailureReason.messageResource: Int
    get() = when (this) {
        NetworkFailureReason.ConnectionRefused -> R.string.auth_message_connection_refused
        NetworkFailureReason.HostUnresolved -> R.string.auth_message_host_unresolved
        NetworkFailureReason.Timeout -> R.string.auth_message_connection_timeout
        NetworkFailureReason.SecureConnectionFailed -> R.string.auth_message_secure_connection_failed
        NetworkFailureReason.NoRoute -> R.string.auth_message_no_route
        NetworkFailureReason.ConnectionLost -> R.string.auth_message_connection_lost
        NetworkFailureReason.Unknown -> R.string.auth_message_network_unavailable
    }

private fun String.userFacingServerMessageOrNull(): String? {
    val normalized = trim()
    return normalized.takeIf { message ->
        message.isNotEmpty() &&
            message.length <= MAX_SERVER_MESSAGE_LENGTH &&
            message.none(Char::isISOControl) &&
            message.any { it in '\u3400'..'\u9FFF' }
    }
}

@StringRes
internal fun AuthField.labelRes(): Int = when (this) {
    AuthField.Username -> R.string.auth_username
    AuthField.DisplayName -> R.string.auth_display_name
    AuthField.Password -> R.string.auth_password
    AuthField.ConfirmPassword -> R.string.auth_confirm_password
}

private const val MAX_SERVER_MESSAGE_LENGTH = 240
