package com.xymusic.app.data.network

import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.network.ProblemMapper
import com.xymusic.app.core.network.isServiceUnavailableStatus
import com.xymusic.app.core.network.model.ProblemDetailsDto
import javax.inject.Inject
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json

class ProblemResponseParser
@Inject
constructor(private val json: Json, private val problemMapper: ProblemMapper) {
    fun parse(status: Int, body: String?, traceId: String?, retryAfterSeconds: Long?): DomainError {
        val problem =
            try {
                body
                    ?.takeIf(String::isNotBlank)
                    ?.let { json.decodeFromString<ProblemDetailsDto>(it) }
            } catch (_: SerializationException) {
                null
            } catch (_: IllegalArgumentException) {
                null
            }
        return problem
            ?.takeIf { it.status == status }
            ?.let { problemMapper.map(it, retryAfterSeconds) }
            ?: fallback(status, traceId, retryAfterSeconds)
    }

    private fun fallback(status: Int, traceId: String?, retryAfterSeconds: Long?): DomainError = when {
        status == 401 ->
            DomainError.Authentication(
                detail = "Authentication failed",
                traceId = traceId,
                reason = com.xymusic.app.core.network.model.ProblemCode.Unknown,
            )
        status == 403 ->
            DomainError.PermissionDenied(
                detail = "Permission denied",
                traceId = traceId,
                reason = com.xymusic.app.core.network.model.ProblemCode.Unknown,
            )
        status == 404 -> DomainError.NotFound("Resource not found", traceId)
        status == 429 -> DomainError.RateLimited("Rate limit exceeded", traceId, retryAfterSeconds)
        status.isServiceUnavailableStatus() -> DomainError.ServiceUnavailable("Service unavailable", traceId)
        status >= 500 -> DomainError.Server("Server request failed", traceId)
        else -> DomainError.Protocol("Unexpected HTTP response", traceId, status)
    }
}
