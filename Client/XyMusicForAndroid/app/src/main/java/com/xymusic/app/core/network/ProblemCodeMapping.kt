package com.xymusic.app.core.network

import com.xymusic.app.core.network.model.ProblemCode
import com.xymusic.app.domain.error.DomainErrorReason

@Suppress("CyclomaticComplexMethod")
fun ProblemCode.toDomainReason(): DomainErrorReason = when (this) {
    ProblemCode.ValidationError -> DomainErrorReason.ValidationError
    ProblemCode.InvalidCursor -> DomainErrorReason.InvalidCursor
    ProblemCode.AuthenticationRequired -> DomainErrorReason.AuthenticationRequired
    ProblemCode.AccessTokenExpired -> DomainErrorReason.AccessTokenExpired
    ProblemCode.SessionRevoked -> DomainErrorReason.SessionRevoked
    ProblemCode.InvalidCredentials -> DomainErrorReason.InvalidCredentials
    ProblemCode.AccountSuspended -> DomainErrorReason.AccountSuspended
    ProblemCode.Forbidden -> DomainErrorReason.Forbidden
    ProblemCode.ResourceNotFound -> DomainErrorReason.ResourceNotFound
    ProblemCode.DuplicateUsername -> DomainErrorReason.DuplicateUsername
    ProblemCode.IdempotencyKeyReused -> DomainErrorReason.IdempotencyKeyReused
    ProblemCode.VersionConflict -> DomainErrorReason.VersionConflict
    ProblemCode.ResourceConflict -> DomainErrorReason.ResourceConflict
    ProblemCode.InvalidStateTransition -> DomainErrorReason.InvalidStateTransition
    ProblemCode.TrackNotPlayable -> DomainErrorReason.TrackNotPlayable
    ProblemCode.TrackAlreadyInPlaylist -> DomainErrorReason.TrackAlreadyInPlaylist
    ProblemCode.MediaUploadMismatch -> DomainErrorReason.MediaUploadMismatch
    ProblemCode.PayloadTooLarge -> DomainErrorReason.PayloadTooLarge
    ProblemCode.RateLimited -> DomainErrorReason.RateLimited
    ProblemCode.DependencyUnavailable -> DomainErrorReason.DependencyUnavailable
    ProblemCode.InternalError -> DomainErrorReason.InternalError
    ProblemCode.Unknown -> DomainErrorReason.Unknown
}

fun DomainErrorReason.toWireValue(): String = ProblemCode.entries.first { it.toDomainReason() == this }.wireValue
