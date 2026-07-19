package com.xymusic.app.core.network.model

import kotlinx.serialization.Serializable

@Serializable
data class ProblemDetailsDto(
    val type: String,
    val title: String,
    val status: Int,
    val code: String,
    val detail: String,
    val instance: String? = null,
    val traceId: String,
    val fieldErrors: Map<String, List<String>> = emptyMap(),
)

enum class ProblemCode(val wireValue: String) {
    ValidationError("VALIDATION_ERROR"),
    InvalidCursor("INVALID_CURSOR"),
    AuthenticationRequired("AUTHENTICATION_REQUIRED"),
    AccessTokenExpired("ACCESS_TOKEN_EXPIRED"),
    SessionRevoked("SESSION_REVOKED"),
    InvalidCredentials("INVALID_CREDENTIALS"),
    AccountSuspended("ACCOUNT_SUSPENDED"),
    Forbidden("FORBIDDEN"),
    ResourceNotFound("RESOURCE_NOT_FOUND"),
    DuplicateUsername("DUPLICATE_USERNAME"),
    IdempotencyKeyReused("IDEMPOTENCY_KEY_REUSED"),
    VersionConflict("VERSION_CONFLICT"),
    ResourceConflict("RESOURCE_CONFLICT"),
    InvalidStateTransition("INVALID_STATE_TRANSITION"),
    TrackNotPlayable("TRACK_NOT_PLAYABLE"),
    TrackAlreadyInPlaylist("TRACK_ALREADY_IN_PLAYLIST"),
    MediaUploadMismatch("MEDIA_UPLOAD_MISMATCH"),
    PayloadTooLarge("PAYLOAD_TOO_LARGE"),
    RateLimited("RATE_LIMITED"),
    DependencyUnavailable("DEPENDENCY_UNAVAILABLE"),
    InternalError("INTERNAL_ERROR"),
    Unknown("UNKNOWN"),
    ;

    companion object {
        fun fromWire(value: String): ProblemCode = entries.firstOrNull { it.wireValue == value } ?: Unknown
    }
}
