package com.xymusic.app.core.network

import com.xymusic.app.core.network.model.ProblemCode

sealed interface DomainError {
    val detail: String
    val traceId: String?

    data class Validation(
        override val detail: String,
        override val traceId: String?,
        val fieldErrors: Map<String, List<String>>,
        val reason: ProblemCode = ProblemCode.ValidationError,
    ) : DomainError

    data class Authentication(override val detail: String, override val traceId: String?, val reason: ProblemCode) :
        DomainError

    data class PermissionDenied(override val detail: String, override val traceId: String?, val reason: ProblemCode) :
        DomainError

    data class NotFound(override val detail: String, override val traceId: String?) : DomainError

    data class Conflict(override val detail: String, override val traceId: String?, val reason: ProblemCode) :
        DomainError

    data class RateLimited(override val detail: String, override val traceId: String?, val retryAfterSeconds: Long?) :
        DomainError

    data class ServiceUnavailable(override val detail: String, override val traceId: String?) : DomainError

    data class Network(
        override val detail: String,
        override val traceId: String? = null,
        val reason: NetworkFailureReason = NetworkFailureReason.Unknown,
    ) : DomainError

    data class Server(override val detail: String, override val traceId: String?) : DomainError

    data class Protocol(override val detail: String, override val traceId: String?, val status: Int?) : DomainError

    data class Local(override val detail: String, override val traceId: String? = null) : DomainError
}

enum class NetworkFailureReason {
    ConnectionRefused,
    HostUnresolved,
    Timeout,
    SecureConnectionFailed,
    NoRoute,
    ConnectionLost,
    Unknown,
}
