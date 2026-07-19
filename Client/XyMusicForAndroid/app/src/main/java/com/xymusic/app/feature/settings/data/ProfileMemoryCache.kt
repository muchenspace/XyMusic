package com.xymusic.app.feature.settings.data

import com.xymusic.app.core.network.ServerGeneration
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.feature.settings.domain.model.UserProfile
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.combine

@Singleton
class ProfileMemoryCache
@Inject
constructor(private val serverRuntimeCoordinator: ServerRuntimeCoordinator) {
    private val cachedProfile = MutableStateFlow<OwnedProfile?>(null)

    fun observe(ownerUserId: String): Flow<UserProfile?> = combine(
        cachedProfile,
        serverRuntimeCoordinator.state,
    ) { cached, _ ->
        cached
            ?.takeIf { entry ->
                entry.ownerUserId == ownerUserId &&
                    serverRuntimeCoordinator.isCurrent(entry.serverGeneration)
            }?.profile
    }

    fun current(ownerUserId: String): UserProfile? =
        cachedProfile.value
            ?.takeIf { entry ->
                entry.ownerUserId == ownerUserId &&
                    serverRuntimeCoordinator.isCurrent(entry.serverGeneration)
            }?.profile

    fun put(ownerUserId: String, serverGeneration: ServerGeneration, profile: UserProfile) {
        serverRuntimeCoordinator.requireCurrent(serverGeneration)
        cachedProfile.value = OwnedProfile(ownerUserId, serverGeneration, profile)
    }

    fun clear() {
        cachedProfile.value = null
    }

    private data class OwnedProfile(
        val ownerUserId: String,
        val serverGeneration: ServerGeneration,
        val profile: UserProfile,
    )
}
