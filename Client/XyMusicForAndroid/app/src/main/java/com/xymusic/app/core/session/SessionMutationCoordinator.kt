package com.xymusic.app.core.session

import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

@Singleton
class SessionMutationCoordinator
@Inject
constructor() {
    private val mutex = Mutex()

    suspend fun <T> mutate(block: suspend () -> T): T = mutex.withLock {
        block()
    }
}
