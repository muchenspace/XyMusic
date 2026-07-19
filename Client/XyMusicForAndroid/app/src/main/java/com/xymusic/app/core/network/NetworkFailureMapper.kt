package com.xymusic.app.core.network

import java.io.EOFException
import java.io.IOException
import java.io.InterruptedIOException
import java.net.ConnectException
import java.net.NoRouteToHostException
import java.net.PortUnreachableException
import java.net.SocketException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import java.net.UnknownServiceException
import javax.net.ssl.SSLException
import javax.net.ssl.SSLPeerUnverifiedException

internal fun IOException.toDomainNetworkError(): DomainError.Network {
    val reason = networkFailureReason()
    return DomainError.Network(
        detail = reason.internalDetail,
        reason = reason,
    )
}

private fun IOException.networkFailureReason(): NetworkFailureReason {
    val failures = generateSequence<Throwable>(this) { it.cause }.toList()
    return when {
        failures.any { it is UnknownHostException } -> NetworkFailureReason.HostUnresolved
        failures.any { it is SocketTimeoutException || it is InterruptedIOException } -> NetworkFailureReason.Timeout
        failures.any {
            it is SSLPeerUnverifiedException ||
                it is SSLException ||
                it.isCleartextRejected()
        } -> NetworkFailureReason.SecureConnectionFailed
        failures.any { it is NoRouteToHostException || it is PortUnreachableException } ->
            NetworkFailureReason.NoRoute
        failures.any { it is ConnectException && it.isConnectionRefused() } ->
            NetworkFailureReason.ConnectionRefused
        failures.any { it is EOFException || it is SocketException } -> NetworkFailureReason.ConnectionLost
        failures.any { it is ConnectException } -> NetworkFailureReason.ConnectionRefused
        else -> NetworkFailureReason.Unknown
    }
}

private fun Throwable.isCleartextRejected(): Boolean =
    this is UnknownServiceException && message.orEmpty().contains("cleartext", ignoreCase = true)

private fun Throwable.isConnectionRefused(): Boolean = message.orEmpty().contains("refused", ignoreCase = true) ||
    message.orEmpty().contains("ECONNREFUSED", ignoreCase = true)

private val NetworkFailureReason.internalDetail: String
    get() = when (this) {
        NetworkFailureReason.ConnectionRefused -> "Server refused the connection"
        NetworkFailureReason.HostUnresolved -> "Server address could not be resolved"
        NetworkFailureReason.Timeout -> "Server connection timed out"
        NetworkFailureReason.SecureConnectionFailed -> "Secure server connection failed"
        NetworkFailureReason.NoRoute -> "Server network route is unavailable"
        NetworkFailureReason.ConnectionLost -> "Server connection was interrupted"
        NetworkFailureReason.Unknown -> "Unable to reach the server"
    }
