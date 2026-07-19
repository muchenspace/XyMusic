package com.xymusic.app.core.session

import kotlinx.coroutines.flow.StateFlow

interface AppSessionProvider {
    val sessionState: StateFlow<AppSessionState>

    suspend fun restoreSession()
}

fun interface SessionIdentityProvider {
    fun activeIdentity(): ActiveSessionIdentity?
}

data class ActiveSessionIdentity(
    val userId: String,
    val sessionId: String,
    val serverGeneration: com.xymusic.app.core.network.ServerGeneration,
)
