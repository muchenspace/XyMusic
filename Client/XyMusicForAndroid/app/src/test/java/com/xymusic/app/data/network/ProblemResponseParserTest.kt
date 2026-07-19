package com.xymusic.app.data.network

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.network.ProblemMapper
import com.xymusic.app.core.network.model.ProblemCode
import com.xymusic.app.core.network.model.ProblemDetailsDto
import kotlinx.serialization.json.Json
import org.junit.Test

class ProblemResponseParserTest {
    private val json = Json { ignoreUnknownKeys = true }
    private val parser = ProblemResponseParser(json, ProblemMapper())

    @Test
    fun mismatchedProblemStatusFallsBackToActualHttpStatus() {
        val body =
            json.encodeToString(
                ProblemDetailsDto(
                    type = "https://example.test/problems/access-token-expired",
                    title = "Authentication failed",
                    status = 401,
                    code = ProblemCode.AccessTokenExpired.wireValue,
                    detail = "Expired",
                    traceId = "body-trace",
                ),
            )

        val result =
            parser.parse(
                status = 500,
                body = body,
                traceId = "header-trace",
                retryAfterSeconds = null,
            )

        assertThat(result).isEqualTo(DomainError.Server("Server request failed", "header-trace"))
    }

    @Test
    fun matchingProblemStatusUsesStructuredProblem() {
        val body =
            json.encodeToString(
                ProblemDetailsDto(
                    type = "https://example.test/problems/access-token-expired",
                    title = "Authentication failed",
                    status = 401,
                    code = ProblemCode.AccessTokenExpired.wireValue,
                    detail = "Expired",
                    traceId = "body-trace",
                ),
            )

        val result = parser.parse(401, body, "header-trace", null)

        assertThat(result).isEqualTo(
            DomainError.Authentication("Expired", "body-trace", ProblemCode.AccessTokenExpired),
        )
    }

    @Test
    fun gatewayUnavailableResponsesDoNotBecomeInputErrors() {
        listOf(502, 503, 504, 521, 522, 523, 524).forEach { status ->
            val result = parser.parse(status, "<html>gateway unavailable</html>", "gateway-trace", null)

            assertThat(result).isEqualTo(
                DomainError.ServiceUnavailable("Service unavailable", "gateway-trace"),
            )
        }
    }
}
