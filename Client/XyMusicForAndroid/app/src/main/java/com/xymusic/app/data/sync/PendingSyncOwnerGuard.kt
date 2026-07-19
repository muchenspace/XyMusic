package com.xymusic.app.data.sync

import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionMutationCoordinator

internal class PendingSyncOwnerGuard(
    private val sessionProvider: AppSessionProvider,
    private val sessionMutationCoordinator: SessionMutationCoordinator,
) {
    fun ensureActive(ownerUserId: String) {
        val activeOwner = (sessionProvider.sessionState.value as? AppSessionState.SignedIn)?.userId
        if (activeOwner != ownerUserId) throw PendingSyncOwnerChangedException
    }

    suspend fun <T> mutateIfActive(ownerUserId: String, block: suspend () -> T): T = sessionMutationCoordinator.mutate {
        ensureActive(ownerUserId)
        block()
    }
}

internal object PendingSyncOwnerChangedException : IllegalStateException()
