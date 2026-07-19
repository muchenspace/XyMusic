package com.xymusic.app.core.database

import android.app.Application
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.database.entity.PendingSyncOperationEntity
import com.xymusic.app.core.database.model.SyncOperationStatus
import com.xymusic.app.core.database.model.SyncOperationType
import com.xymusic.app.core.database.model.SyncTargetType
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class PendingSyncOperationDaoTest {
    private lateinit var database: XyMusicDatabase

    @Before
    fun setUp() {
        database =
            Room
                .inMemoryDatabaseBuilder(
                    ApplicationProvider.getApplicationContext(),
                    XyMusicDatabase::class.java,
                ).allowMainThreadQueries()
                .build()
    }

    @After
    fun tearDown() {
        database.close()
    }

    @Test
    fun queueIsIdempotentLeasedRetryableAndOwnerScoped() = runTest {
        val dao = database.pendingSyncOperationDao()
        assertThat(dao.enqueue(operation("alice", "operation-1", "same-key"))).isNotEqualTo(-1)
        assertThat(dao.enqueue(operation("alice", "operation-2", "same-key"))).isEqualTo(-1)
        assertThat(dao.enqueue(operation("bob", "operation-3", "same-key"))).isNotEqualTo(-1)

        val claimed = dao.claimNext("alice", "worker-1", nowEpochMs = 1_000, leaseDurationMs = 500)

        assertThat(claimed?.id).isEqualTo("operation-1")
        assertThat(claimed?.status).isEqualTo(SyncOperationStatus.RUNNING)
        assertThat(claimed?.attemptCount).isEqualTo(1)
        assertThat(dao.claimNext("alice", "worker-2", 1_100, 500)).isNull()
        assertThat(dao.observeAll("bob").first().map { it.id }).containsExactly("operation-3")

        assertThat(
            dao.markFailed(
                ownerUserId = "alice",
                operationId = "operation-1",
                leaseOwner = "worker-1",
                errorCode = "NETWORK_UNAVAILABLE",
                nowEpochMs = 1_100,
                nextAttemptAtEpochMs = 2_000,
            ),
        ).isEqualTo(1)
        assertThat(dao.claimNext("alice", "worker-2", 1_999, 500)).isNull()

        val retried = dao.claimNext("alice", "worker-2", 2_000, 500)
        assertThat(retried?.attemptCount).isEqualTo(2)
        assertThat(dao.complete("alice", "operation-1", "worker-1")).isEqualTo(0)
        assertThat(dao.complete("alice", "operation-1", "worker-2")).isEqualTo(1)
        assertThat(dao.observeAll("alice").first()).isEmpty()
    }

    @Test
    fun expiredLeaseCanBeReclaimed() = runTest {
        val dao = database.pendingSyncOperationDao()
        dao.enqueue(operation("alice", "operation-1", "key-1"))
        dao.claimNext("alice", "worker-1", nowEpochMs = 1_000, leaseDurationMs = 500)

        val reclaimed = dao.claimNext("alice", "worker-2", nowEpochMs = 1_500, leaseDurationMs = 500)

        assertThat(reclaimed?.leaseOwner).isEqualTo("worker-2")
        assertThat(reclaimed?.attemptCount).isEqualTo(2)
    }

    @Test
    fun unattemptedPlaybackProgressIsCoalescedButRunningProgressIsPreserved() = runTest {
        val dao = database.pendingSyncOperationDao()
        val prefix = "playback-progress:session-1:"
        dao.enqueueOrReplacePlaybackProgress(
            playbackProgress("operation-1", prefix + "key-1", "position-1", 1_000),
            prefix,
        )
        dao.enqueueOrReplacePlaybackProgress(
            playbackProgress("operation-2", prefix + "key-2", "position-2", 2_000),
            prefix,
        )

        val coalesced = dao.observeAll("alice").first()
        assertThat(coalesced).hasSize(1)
        assertThat(coalesced.single().id).isEqualTo("operation-1")
        assertThat(coalesced.single().requestPayloadJson).isEqualTo("position-2")
        assertThat(coalesced.single().updatedAtEpochMs).isEqualTo(2_000)

        dao.claimNext("alice", "worker-1", nowEpochMs = 2_000, leaseDurationMs = 500)
        dao.enqueueOrReplacePlaybackProgress(
            playbackProgress("operation-3", prefix + "key-3", "position-3", 3_000),
            prefix,
        )

        val withRunning = dao.observeAll("alice").first()
        assertThat(withRunning.map { it.id }).containsExactly("operation-1", "operation-3")
        assertThat(withRunning.first { it.id == "operation-1" }.requestPayloadJson)
            .isEqualTo("position-2")
        assertThat(withRunning.first { it.id == "operation-3" }.requestPayloadJson)
            .isEqualTo("position-3")
    }

    private fun operation(
        owner: String,
        id: String,
        key: String,
        targetType: SyncTargetType = SyncTargetType.FAVORITE,
        targetId: String? = "track-1",
        status: SyncOperationStatus = SyncOperationStatus.PENDING,
    ) = PendingSyncOperationEntity(
        ownerUserId = owner,
        id = id,
        operationType =
        if (targetType == SyncTargetType.PLAYLIST) {
            SyncOperationType.UPDATE_PLAYLIST
        } else {
            SyncOperationType.ADD_FAVORITE
        },
        targetType = targetType,
        targetId = targetId,
        requestPayloadJson = null,
        idempotencyKey = key,
        status = status,
        attemptCount = 0,
        createdAtEpochMs = 1_000,
        updatedAtEpochMs = 1_000,
        nextAttemptAtEpochMs = 1_000,
        leaseOwner = null,
        leaseExpiresAtEpochMs = null,
        lastErrorCode = null,
    )

    private fun playbackProgress(id: String, key: String, payload: String, time: Long) = PendingSyncOperationEntity(
        ownerUserId = "alice",
        id = id,
        operationType = SyncOperationType.RECORD_PLAYBACK,
        targetType = SyncTargetType.PLAYBACK_HISTORY,
        targetId = "track-1",
        requestPayloadJson = payload,
        idempotencyKey = key,
        status = SyncOperationStatus.PENDING,
        attemptCount = 0,
        createdAtEpochMs = time,
        updatedAtEpochMs = time,
        nextAttemptAtEpochMs = time,
        leaseOwner = null,
        leaseExpiresAtEpochMs = null,
        lastErrorCode = null,
    )
}
