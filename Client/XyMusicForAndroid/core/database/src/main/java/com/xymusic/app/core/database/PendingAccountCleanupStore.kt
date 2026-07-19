package com.xymusic.app.core.database

interface PendingAccountCleanupStore {
    fun owners(): Set<String>

    fun add(ownerUserId: String)

    fun remove(ownerUserId: String)
}
