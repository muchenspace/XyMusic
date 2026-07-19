package com.xymusic.app.data.network.auth.model

import com.xymusic.app.core.security.AccessToken
import com.xymusic.app.core.security.RefreshToken
import com.xymusic.app.core.security.SessionTokens
import java.time.Instant
import java.util.UUID

fun AuthSessionDto.toSessionTokens(nowEpochMillis: Long): SessionTokens {
    require(tokens.tokenType == TOKEN_TYPE_BEARER) { "Unsupported token type" }
    requireUuid(user.id, "user ID")
    requireUuid(session.id, "session ID")
    require(user.version >= 1) { "Invalid user version" }
    require(user.role in VALID_ROLES) { "Invalid user role" }
    require(user.status in VALID_USER_STATUSES) { "Invalid user status" }
    Instant.parse(user.createdAt)
    Instant.parse(user.updatedAt)
    Instant.parse(session.createdAt)
    require(tokens.accessToken.length >= MIN_TOKEN_LENGTH) { "Access token is too short" }
    require(tokens.refreshToken.length in MIN_TOKEN_LENGTH..MAX_REFRESH_TOKEN_LENGTH) {
        "Refresh token length is invalid"
    }
    val accessExpiresAt = Instant.parse(tokens.accessTokenExpiresAt).toEpochMilli()
    val refreshExpiresAt = Instant.parse(tokens.refreshTokenExpiresAt).toEpochMilli()
    require(accessExpiresAt > nowEpochMillis) { "Access token is already expired" }
    require(refreshExpiresAt > accessExpiresAt) {
        "Refresh token must outlive the access token"
    }
    return SessionTokens(
        userId = user.id,
        sessionId = session.id,
        accessToken = AccessToken.from(tokens.accessToken),
        accessTokenExpiresAtEpochMillis = accessExpiresAt,
        refreshToken = RefreshToken.from(tokens.refreshToken),
        refreshTokenExpiresAtEpochMillis = refreshExpiresAt,
    )
}

private fun requireUuid(value: String, fieldName: String) {
    require(runCatching { UUID.fromString(value) }.isSuccess) { "Invalid $fieldName" }
}

private const val TOKEN_TYPE_BEARER = "Bearer"
private const val MIN_TOKEN_LENGTH = 32
private const val MAX_REFRESH_TOKEN_LENGTH = 4096
private val VALID_ROLES = setOf("USER", "ADMIN")
private val VALID_USER_STATUSES =
    setOf(
        "ACTIVE",
        "SUSPENDED",
        "DELETED",
    )
