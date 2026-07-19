package com.xymusic.app.core.database

import android.app.Application
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.database.entity.FavoriteEntity
import com.xymusic.app.core.database.entity.HistoryEntity
import com.xymusic.app.core.database.entity.PendingSyncOperationEntity
import com.xymusic.app.core.database.entity.PlaybackQueueEntity
import com.xymusic.app.core.database.entity.PlaylistEntity
import com.xymusic.app.core.database.entity.PlaylistEntryEntity
import com.xymusic.app.core.database.entity.SearchHistoryEntity
import com.xymusic.app.core.database.model.PlaylistVisibility
import com.xymusic.app.core.database.model.SearchScope
import com.xymusic.app.core.database.model.SyncOperationStatus
import com.xymusic.app.core.database.model.SyncOperationType
import com.xymusic.app.core.database.model.SyncTargetType
import dagger.Lazy
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
class AccountDataCleanerTest {
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
    fun logoutDeletesOnlyTheSelectedOwnersPrivateData() = runTest {
        database.seedTrack("track-1")
        seedOwner("alice")
        seedOwner("bob")
        var clearedOfflineOwner: String? = null
        val cleaner =
            RoomAccountDataCleaner(
                database.accountDataDao(),
                Lazy {
                    OfflineAccountDataCleaner { ownerUserId ->
                        clearedOfflineOwner = ownerUserId
                        1
                    }
                },
            )

        val deletion = cleaner.clear("alice")

        assertThat(deletion.totalCount).isEqualTo(8)
        assertThat(deletion.offlineTrackCount).isEqualTo(1)
        assertThat(clearedOfflineOwner).isEqualTo("alice")
        assertOwnerIsEmpty("alice")
        assertOwnerIsPresent("bob")
        assertThat(database.catalogDao().track("track-1")).isNotNull()
    }

    private suspend fun seedOwner(owner: String) {
        database.libraryDao().upsertFavorite(FavoriteEntity(owner, "track-1", 1_000))
        database.libraryDao().upsertHistory(
            HistoryEntity(owner, "track-1", 0, 1, 1_000, false, 1_000),
        )
        val playlist =
            PlaylistEntity(
                ownerUserId = owner,
                id = "playlist-$owner",
                name = "Playlist",
                description = null,
                visibility = PlaylistVisibility.PRIVATE,
                cover = null,
                trackCount = 1,
                version = 1,
                createdAtEpochMs = 1_000,
                updatedAtEpochMs = 1_000,
            )
        database.playlistDao().replacePlaylist(
            playlist,
            listOf(
                PlaylistEntryEntity(owner, "entry-$owner", playlist.id, 0, "track-1", owner, 1_000),
            ),
        )
        database.playbackQueueDao().replace(
            owner,
            listOf(PlaybackQueueEntity(owner, "queue-$owner", 0, "track-1", null, null, 0, true, 1_000)),
        )
        database.searchHistoryDao().record(
            SearchHistoryEntity(owner, "track", SearchScope.ALL, "Track", 1_000),
        )
        database.pendingSyncOperationDao().enqueue(operation(owner))
    }

    private suspend fun assertOwnerIsEmpty(owner: String) {
        assertThat(database.libraryDao().observeFavorite(owner, "track-1").first()).isFalse()
        assertThat(database.libraryDao().observeHistory(owner).first()).isEmpty()
        assertThat(database.playlistDao().observePlaylists(owner).first()).isEmpty()
        assertThat(database.playbackQueueDao().observe(owner).first()).isEmpty()
        assertThat(database.searchHistoryDao().observe(owner).first()).isEmpty()
        assertThat(database.pendingSyncOperationDao().observeAll(owner).first()).isEmpty()
    }

    private suspend fun assertOwnerIsPresent(owner: String) {
        assertThat(database.libraryDao().observeFavorite(owner, "track-1").first()).isTrue()
        assertThat(database.libraryDao().observeHistory(owner).first()).hasSize(1)
        assertThat(database.playlistDao().observePlaylists(owner).first()).hasSize(1)
        assertThat(database.playbackQueueDao().observe(owner).first()).hasSize(1)
        assertThat(database.searchHistoryDao().observe(owner).first()).hasSize(1)
        assertThat(database.pendingSyncOperationDao().observeAll(owner).first()).hasSize(1)
    }

    private fun operation(owner: String) = PendingSyncOperationEntity(
        ownerUserId = owner,
        id = "operation-$owner",
        operationType = SyncOperationType.ADD_FAVORITE,
        targetType = SyncTargetType.FAVORITE,
        targetId = "track-1",
        requestPayloadJson = null,
        idempotencyKey = "idempotency-$owner",
        status = SyncOperationStatus.PENDING,
        attemptCount = 0,
        createdAtEpochMs = 1_000,
        updatedAtEpochMs = 1_000,
        nextAttemptAtEpochMs = 1_000,
        leaseOwner = null,
        leaseExpiresAtEpochMs = null,
        lastErrorCode = null,
    )
}
