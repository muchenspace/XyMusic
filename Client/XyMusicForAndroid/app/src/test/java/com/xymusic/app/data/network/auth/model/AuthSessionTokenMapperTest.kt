package com.xymusic.app.data.network.auth.model

import com.google.common.truth.Truth.assertThat
import java.time.Instant
import org.junit.Test

class AuthSessionTokenMapperTest {
    @Test
    fun mapsAuthenticationSessionToSecureTokenModel() {
        val session = session()

        val tokens = session.toSessionTokens(Instant.parse("2026-07-11T00:00:00Z").toEpochMilli())

        assertThat(tokens.userId).isEqualTo(USER_ID)
        assertThat(tokens.sessionId).isEqualTo(SESSION_ID)
        assertThat(tokens.accessToken.value).isEqualTo(ACCESS_TOKEN)
        assertThat(tokens.refreshToken.value).isEqualTo(REFRESH_TOKEN)
        assertThat(tokens.accessTokenExpiresAtEpochMillis)
            .isEqualTo(Instant.parse("2026-07-11T01:00:00Z").toEpochMilli())
        assertThat(tokens.refreshTokenExpiresAtEpochMillis)
            .isEqualTo(Instant.parse("2026-08-11T00:00:00Z").toEpochMilli())
    }

    @Test(expected = IllegalArgumentException::class)
    fun rejectsUnsupportedTokenType() {
        session(tokenType = "Basic")
            .toSessionTokens(Instant.parse("2026-07-11T00:00:00Z").toEpochMilli())
    }

    @Test(expected = IllegalArgumentException::class)
    fun rejectsMismatchedExpiryOrdering() {
        session(
            accessExpiresAt = "2026-08-12T00:00:00Z",
            refreshExpiresAt = "2026-08-11T00:00:00Z",
        ).toSessionTokens(Instant.parse("2026-07-11T00:00:00Z").toEpochMilli())
    }

    private fun session(
        tokenType: String = "Bearer",
        accessExpiresAt: String = "2026-07-11T01:00:00Z",
        refreshExpiresAt: String = "2026-08-11T00:00:00Z",
    ): AuthSessionDto = AuthSessionDto(
        user =
        AuthUserDto(
            id = USER_ID,
            username = "alice_01",
            displayName = "Alice",
            bio = null,
            avatar = null,
            role = "USER",
            status = "ACTIVE",
            version = 1,
            createdAt = "2026-07-10T00:00:00Z",
            updatedAt = "2026-07-10T00:00:00Z",
        ),
        session =
        AuthSessionInfoDto(
            id = SESSION_ID,
            deviceName = "Test device",
            createdAt = "2026-07-11T00:00:00Z",
        ),
        tokens =
        TokenPairDto(
            tokenType = tokenType,
            accessToken = ACCESS_TOKEN,
            accessTokenExpiresAt = accessExpiresAt,
            refreshToken = REFRESH_TOKEN,
            refreshTokenExpiresAt = refreshExpiresAt,
        ),
    )

    private companion object {
        const val USER_ID = "11111111-1111-4111-8111-111111111111"
        const val SESSION_ID = "22222222-2222-4222-8222-222222222222"
        const val ACCESS_TOKEN = "access-token-123456789012345678901234"
        const val REFRESH_TOKEN = "refresh-token-12345678901234567890123"
    }
}
