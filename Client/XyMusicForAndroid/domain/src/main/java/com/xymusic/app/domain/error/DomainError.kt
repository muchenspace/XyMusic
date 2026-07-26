package com.xymusic.app.domain.error

sealed interface DomainError {
    val detail: String
    val traceId: String?

    data class Validation(
        override val detail: String,
        override val traceId: String?,
        val fieldErrors: Map<String, List<String>>,
        val reason: DomainErrorReason = DomainErrorReason.ValidationError,
    ) : DomainError

    data class Authentication(
        override val detail: String,
        override val traceId: String?,
        val reason: DomainErrorReason,
    ) : DomainError

    data class PermissionDenied(
        override val detail: String,
        override val traceId: String?,
        val reason: DomainErrorReason,
    ) : DomainError

    data class NotFound(override val detail: String, override val traceId: String?) : DomainError

    data class Conflict(override val detail: String, override val traceId: String?, val reason: DomainErrorReason) :
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

enum class DomainErrorReason {
    ValidationError,
    InvalidCursor,
    AuthenticationRequired,
    AccessTokenExpired,
    SessionRevoked,
    InvalidCredentials,
    AccountSuspended,
    Forbidden,
    ResourceNotFound,
    DuplicateUsername,
    IdempotencyKeyReused,
    VersionConflict,
    ResourceConflict,
    InvalidStateTransition,
    TrackNotPlayable,
    TrackAlreadyInPlaylist,
    MediaUploadMismatch,
    PayloadTooLarge,
    RateLimited,
    DependencyUnavailable,
    InternalError,
    Unknown,
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
