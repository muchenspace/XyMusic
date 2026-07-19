package com.xymusic.app.core.network.auth

import com.xymusic.app.core.security.RefreshToken

interface RefreshAttemptStore {
    fun idempotencyKeyFor(refreshToken: RefreshToken): String

    fun clear()
}
