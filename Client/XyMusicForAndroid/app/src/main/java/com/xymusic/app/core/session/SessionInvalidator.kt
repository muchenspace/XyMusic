package com.xymusic.app.core.session

fun interface SessionInvalidator {
    /** Stops the active session, then clears credentials and every owner-scoped local record. */
    suspend fun invalidateSession(ownerUserId: String?)

    /** Clears credentials only if the caller's captured session is still active. */
    suspend fun invalidateSessionIfCurrent(expectedIdentity: ActiveSessionIdentity) {
        invalidateSession(expectedIdentity.userId)
    }
}
