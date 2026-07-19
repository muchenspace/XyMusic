package com.xymusic.app.feature.player.data.local

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.database.dao.PlaybackQueueDao
import com.xymusic.app.core.database.entity.PlaybackQueueEntity
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.feature.player.domain.PlayerResult
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import org.junit.Test

class RoomPlaybackQueueStoreTest {
    @Test
    fun staleOwnerCannotReplaceNewOwnersQueue() = runTest {
        val dao = RecordingPlaybackQueueDao()
        val session = MutableSessionProvider(AppSessionState.SignedIn("owner-b"))
        val store =
            RoomPlaybackQueueStore(
                playbackQueueDao = dao,
                appSessionProvider = session,
                sessionMutationCoordinator = SessionMutationCoordinator(),
                json = Json,
                defaultDispatcher = StandardTestDispatcher(testScheduler),
            )

        val result = store.replace(ownerUserId = "owner-a", items = emptyList())

        assertThat(result).isInstanceOf(PlayerResult.Failure::class.java)
        assertThat(dao.clearedOwners).isEmpty()
    }

    @Test
    fun matchingOwnerCanPersistQueue() = runTest {
        val dao = RecordingPlaybackQueueDao()
        val session = MutableSessionProvider(AppSessionState.SignedIn("owner-a"))
        val store =
            RoomPlaybackQueueStore(
                playbackQueueDao = dao,
                appSessionProvider = session,
                sessionMutationCoordinator = SessionMutationCoordinator(),
                json = Json,
                defaultDispatcher = StandardTestDispatcher(testScheduler),
            )

        val result = store.replace(ownerUserId = "owner-a", items = emptyList())

        assertThat(result).isInstanceOf(PlayerResult.Success::class.java)
        assertThat(dao.clearedOwners).containsExactly("owner-a")
    }

    @Test
    fun matchingOwnerCanUpdateCurrentItemWithoutReplacingQueue() = runTest {
        val dao = RecordingPlaybackQueueDao()
        val session = MutableSessionProvider(AppSessionState.SignedIn("owner-a"))
        val store =
            RoomPlaybackQueueStore(
                playbackQueueDao = dao,
                appSessionProvider = session,
                sessionMutationCoordinator = SessionMutationCoordinator(),
                json = Json,
                defaultDispatcher = StandardTestDispatcher(testScheduler),
            )

        val result =
            store.setCurrent(
                ownerUserId = "owner-a",
                queueItemId = "queue-1",
                positionMs = 42_000,
            )

        assertThat(result).isInstanceOf(PlayerResult.Success::class.java)
        assertThat(dao.currentUpdates).containsExactly(CurrentUpdate("owner-a", "queue-1", 42_000))
        assertThat(dao.clearedOwners).isEmpty()
    }
}

private data class CurrentUpdate(val ownerUserId: String, val itemId: String, val resumePositionMs: Long)

private class MutableSessionProvider(initialState: AppSessionState) : AppSessionProvider {
    override val sessionState = MutableStateFlow(initialState)

    override suspend fun restoreSession() = Unit
}

private class RecordingPlaybackQueueDao : PlaybackQueueDao() {
    val clearedOwners = mutableListOf<String>()
    val currentUpdates = mutableListOf<CurrentUpdate>()

    override suspend fun insertItems(items: List<PlaybackQueueEntity>) = Unit

    override suspend fun clear(ownerUserId: String): Int {
        clearedOwners += ownerUserId
        return 0
    }

    override fun observe(ownerUserId: String): Flow<List<PlaybackQueueEntity>> = flowOf(emptyList())

    override suspend fun snapshot(ownerUserId: String): List<PlaybackQueueEntity> = emptyList()

    override suspend fun current(ownerUserId: String): PlaybackQueueEntity? = null

    override suspend fun updateResumePositionIfPresent(
        ownerUserId: String,
        itemId: String,
        resumePositionMs: Long,
    ): Int = 1

    override suspend fun clearCurrent(ownerUserId: String): Int = 0

    override suspend fun setCurrentIfPresent(ownerUserId: String, itemId: String, resumePositionMs: Long): Int {
        currentUpdates += CurrentUpdate(ownerUserId, itemId, resumePositionMs)
        return 1
    }
}
