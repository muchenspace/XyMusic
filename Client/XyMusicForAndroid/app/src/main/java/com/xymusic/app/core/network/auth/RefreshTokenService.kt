package com.xymusic.app.core.network.auth

import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.security.RefreshToken
import com.xymusic.app.core.security.SessionTokens
import com.xymusic.app.core.session.ActiveSessionIdentity
import java.io.IOException

fun interface RefreshTokenService {
    @Throws(IOException::class)
    suspend fun refresh(request: RefreshTokenRequest): SessionTokens
}

data class RefreshTokenRequest(val refreshToken: RefreshToken, val expectedIdentity: ActiveSessionIdentity)

class TokenRefreshRejectedException(val domainError: DomainError) : IOException("Token refresh rejected")
