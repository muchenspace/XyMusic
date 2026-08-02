package com.xymusic.app.core.sync

import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionMutationCoordinator
import javax.inject.Inject

class PendingSyncOwnerGuard
@Inject
constructor(
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

object PendingSyncOwnerChangedException : IllegalStateException()
