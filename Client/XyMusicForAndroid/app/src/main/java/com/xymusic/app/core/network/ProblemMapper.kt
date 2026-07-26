package com.xymusic.app.core.network

import com.xymusic.app.core.network.model.ProblemCode
import com.xymusic.app.core.network.model.ProblemDetailsDto
import com.xymusic.app.domain.error.DomainError
import com.xymusic.app.domain.error.DomainErrorReason
import javax.inject.Inject

class ProblemMapper
@Inject
constructor() {
    fun map(problem: ProblemDetailsDto, retryAfterSeconds: Long? = null): DomainError {
        val code = ProblemCode.fromWire(problem.code)
        if (problem.status.isServiceUnavailableStatus()) {
            return DomainError.ServiceUnavailable(problem.detail, problem.traceId)
        }
        return when (code) {
            ProblemCode.ValidationError,
            ProblemCode.InvalidCursor,
            ->
                DomainError.Validation(
                    detail = problem.detail,
                    traceId = problem.traceId,
                    fieldErrors = problem.fieldErrors,
                    reason = code.toDomainReason(),
                )

            ProblemCode.AuthenticationRequired,
            ProblemCode.AccessTokenExpired,
            ProblemCode.SessionRevoked,
            ProblemCode.InvalidCredentials,
            -> DomainError.Authentication(problem.detail, problem.traceId, code.toDomainReason())

            ProblemCode.AccountSuspended,
            ProblemCode.Forbidden,
            -> DomainError.PermissionDenied(problem.detail, problem.traceId, code.toDomainReason())

            ProblemCode.ResourceNotFound ->
                DomainError.NotFound(problem.detail, problem.traceId)

            ProblemCode.DuplicateUsername,
            ProblemCode.IdempotencyKeyReused,
            ProblemCode.VersionConflict,
            ProblemCode.ResourceConflict,
            ProblemCode.InvalidStateTransition,
            ProblemCode.TrackNotPlayable,
            ProblemCode.TrackAlreadyInPlaylist,
            ProblemCode.MediaUploadMismatch,
            ProblemCode.PayloadTooLarge,
            -> DomainError.Conflict(problem.detail, problem.traceId, code.toDomainReason())

            ProblemCode.RateLimited ->
                DomainError.RateLimited(problem.detail, problem.traceId, retryAfterSeconds)

            ProblemCode.DependencyUnavailable ->
                DomainError.ServiceUnavailable(problem.detail, problem.traceId)

            ProblemCode.InternalError -> DomainError.Server(problem.detail, problem.traceId)
            ProblemCode.Unknown -> mapUnknown(problem)
        }
    }

    private fun mapUnknown(problem: ProblemDetailsDto): DomainError = when {
        problem.status.isServiceUnavailableStatus() ->
            DomainError.ServiceUnavailable(problem.detail, problem.traceId)
        problem.status >= 500 -> DomainError.Server(problem.detail, problem.traceId)
        problem.status == 401 ->
            DomainError.Authentication(
                detail = problem.detail,
                traceId = problem.traceId,
                reason = DomainErrorReason.Unknown,
            )
        problem.status == 403 ->
            DomainError.PermissionDenied(
                detail = problem.detail,
                traceId = problem.traceId,
                reason = DomainErrorReason.Unknown,
            )
        problem.status == 404 -> DomainError.NotFound(problem.detail, problem.traceId)
        else -> DomainError.Protocol(problem.detail, problem.traceId, problem.status)
    }
}
