package com.xymusic.app.core.security

data class SessionTokens(
    val userId: String,
    val sessionId: String,
    val accessToken: AccessToken,
    val accessTokenExpiresAtEpochMillis: Long,
    val refreshToken: RefreshToken,
    val refreshTokenExpiresAtEpochMillis: Long,
) {
    fun isRefreshExpired(nowEpochMillis: Long): Boolean = refreshTokenExpiresAtEpochMillis <= nowEpochMillis
}

class AccessToken private constructor(val value: String) {
    init {
        require(value.isNotBlank()) { "Access token must not be blank" }
    }

    override fun equals(other: Any?): Boolean = other is AccessToken && value == other.value

    override fun hashCode(): Int = value.hashCode()

    override fun toString(): String = "AccessToken([REDACTED])"

    companion object {
        fun from(value: String): AccessToken = AccessToken(value)
    }
}

class RefreshToken private constructor(val value: String) {
    init {
        require(value.isNotBlank()) { "Refresh token must not be blank" }
    }

    override fun equals(other: Any?): Boolean = other is RefreshToken && value == other.value

    override fun hashCode(): Int = value.hashCode()

    override fun toString(): String = "RefreshToken([REDACTED])"

    companion object {
        fun from(value: String): RefreshToken = RefreshToken(value)
    }
}
