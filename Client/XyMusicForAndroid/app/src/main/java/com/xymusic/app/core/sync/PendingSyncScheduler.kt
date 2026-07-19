package com.xymusic.app.core.sync

interface PendingSyncScheduler {
    fun schedule(ownerUserId: String)

    fun continueDrain(ownerUserId: String) = schedule(ownerUserId)

    fun cancel(ownerUserId: String)
}
