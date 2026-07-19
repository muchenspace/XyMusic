package com.xymusic.app.core.network

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.network.model.ProblemCode
import com.xymusic.app.core.network.model.ProblemDetailsDto
import org.junit.Test

class ProblemMapperTest {
    private val mapper = ProblemMapper()

    @Test
    fun mapsValidationProblemAndPreservesFieldErrors() {
        val problem =
            problem(
                status = 400,
                code = ProblemCode.ValidationError.wireValue,
                fieldErrors = mapOf("username" to listOf("must be a valid username")),
            )

        val error = mapper.map(problem)

        assertThat(error).isInstanceOf(DomainError.Validation::class.java)
        val validation = error as DomainError.Validation
        assertThat(validation.fieldErrors["username"]).containsExactly("must be a valid username")
        assertThat(validation.traceId).isEqualTo("trace-12345678")
    }

    @Test
    fun mapsRateLimitRetryMetadata() {
        val error =
            mapper.map(
                problem(status = 429, code = ProblemCode.RateLimited.wireValue),
                retryAfterSeconds = 45,
            )

        assertThat(error).isEqualTo(
            DomainError.RateLimited(
                detail = "Request failed",
                traceId = "trace-12345678",
                retryAfterSeconds = 45,
            ),
        )
    }

    @Test
    fun unknownInternalProblemMapsToServerError() {
        val error = mapper.map(problem(status = 500, code = "FUTURE_SERVER_CODE"))

        assertThat(error).isInstanceOf(DomainError.Server::class.java)
    }

    @Test
    fun gatewayUnavailableStatusesMapToServiceUnavailable() {
        listOf(502, 503, 504, 521, 522, 523, 524).forEach { status ->
            val error = mapper.map(problem(status = status, code = "FUTURE_SERVER_CODE"))

            assertThat(error).isInstanceOf(DomainError.ServiceUnavailable::class.java)
        }
    }

    @Test
    fun serviceUnavailableStatusOverridesInternalErrorCode() {
        val error = mapper.map(problem(status = 503, code = ProblemCode.InternalError.wireValue))

        assertThat(error).isInstanceOf(DomainError.ServiceUnavailable::class.java)
    }

    private fun problem(
        status: Int,
        code: String,
        fieldErrors: Map<String, List<String>> = emptyMap(),
    ): ProblemDetailsDto = ProblemDetailsDto(
        type = "https://api.xymusic.example/problems/$code",
        title = "Request failed",
        status = status,
        code = code,
        detail = "Request failed",
        traceId = "trace-12345678",
        fieldErrors = fieldErrors,
    )
}
