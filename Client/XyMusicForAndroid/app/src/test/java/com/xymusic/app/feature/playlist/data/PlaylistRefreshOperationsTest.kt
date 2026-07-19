package com.xymusic.app.feature.playlist.data

import android.app.Application
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.data.media.RoomCatalogLocalDataSource
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.entity.PendingSyncOperationEntity
import com.xymusic.app.core.database.entity.PlaylistEntity
import com.xymusic.app.core.database.entity.PlaylistEntryEntity
import com.xymusic.app.core.database.entity.TrackEntity
import com.xymusic.app.core.database.model.PlaylistVisibility
import com.xymusic.app.core.database.model.SyncOperationStatus
import com.xymusic.app.core.database.model.SyncOperationType
import com.xymusic.app.core.database.model.SyncTargetType
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.feature.playlist.data.remote.AddPlaylistTrackRequestDto
import com.xymusic.app.feature.playlist.data.remote.CreatePlaylistRequestDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistDetailDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistEntryMutationDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistMutationDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistRemoteDataSource
import com.xymusic.app.feature.playlist.data.remote.PlaylistSummaryDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistUpdatePayload
import com.xymusic.app.feature.playlist.data.remote.ReorderPlaylistRequestDto
import com.xymusic.app.feature.playlist.data.remote.UserSummaryDto
import com.xymusic.app.feature.playlist.domain.PlaylistResult
import com.xymusic.app.feature.playlist.domain.model.PlaylistSort
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
@OptIn(ExperimentalCoroutinesApi::class)
class PlaylistRefreshOperationsTest {
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
    fun listRefreshIgnoresLegacyConflictAndInvalidatesEntriesWhenVersionChanges() = runTest {
        database.catalogDao().upsertTrack(track())
        database.playlistDao().replacePlaylist(
            playlist(version = 1, trackCount = 1),
            listOf(entry()),
        )
        database.pendingSyncOperationDao().enqueue(legacyConflict())
        val operations =
            operations(
                remote = RefreshRemote(summaries = listOf(summary(version = 2, trackCount = 1))),
                dispatcher = UnconfinedTestDispatcher(testScheduler),
            )

        val result = operations.refreshPlaylists(PlaylistSort.UPDATED_DESC)

        assertThat(result).isInstanceOf(PlaylistResult.Success::class.java)
        assertThat(database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)?.version).isEqualTo(2)
        assertThat(database.playlistDao().entries(OWNER_ID, PLAYLIST_ID)).isEmpty()
    }

    @Test
    fun detailRefreshIgnoresLegacyConflict() = runTest {
        database.playlistDao().upsertPlaylist(playlist(version = 1, trackCount = 0))
        database.pendingSyncOperationDao().enqueue(legacyConflict())
        val operations =
            operations(
                remote = RefreshRemote(detail = detail(version = 2)),
                dispatcher = UnconfinedTestDispatcher(testScheduler),
            )

        val result = operations.refreshPlaylist(PLAYLIST_ID)

        assertThat(result).isInstanceOf(PlaylistResult.Success::class.java)
        assertThat(database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)?.version).isEqualTo(2)
    }

    private fun operations(
        remote: PlaylistRemoteDataSource,
        dispatcher: kotlinx.coroutines.CoroutineDispatcher,
    ): PlaylistRefreshOperations {
        val executionContext =
            PlaylistRepositoryExecutionContext(
                sessionProvider = SignedInSessionProvider,
                sessionMutationCoordinator = SessionMutationCoordinator(),
                serverRuntimeCoordinator = ServerRuntimeCoordinator(),
                ioDispatcher = dispatcher,
            )
        val localStore =
            PlaylistLocalStore(
                database = database,
                playlistDao = database.playlistDao(),
                catalogDao = database.catalogDao(),
                catalogLocal = RoomCatalogLocalDataSource(database.catalogDao()),
                clock = CLOCK,
                executionContext = executionContext,
            )
        return PlaylistRefreshOperations(
            database = database,
            playlistDao = database.playlistDao(),
            remote = remote,
            executionContext = executionContext,
            localStore = localStore,
        )
    }

    private fun playlist(version: Long, trackCount: Int) = PlaylistEntity(
        ownerUserId = OWNER_ID,
        id = PLAYLIST_ID,
        name = "Playlist",
        description = null,
        visibility = PlaylistVisibility.PRIVATE,
        cover = null,
        trackCount = trackCount,
        version = version,
        createdAtEpochMs = NOW.toEpochMilli(),
        updatedAtEpochMs = NOW.toEpochMilli(),
    )

    private fun entry() = PlaylistEntryEntity(
        ownerUserId = OWNER_ID,
        id = ENTRY_ID,
        playlistId = PLAYLIST_ID,
        position = 0,
        trackId = TRACK_ID,
        addedByUserId = OWNER_ID,
        addedAtEpochMs = NOW.toEpochMilli(),
    )

    private fun track() = TrackEntity(
        id = TRACK_ID,
        albumId = null,
        title = "Track",
        durationMs = 1_000,
        trackNumber = null,
        discNumber = 1,
        publishedAtEpochMs = NOW.toEpochMilli(),
        artwork = null,
        cachedAtEpochMs = NOW.toEpochMilli(),
    )

    private fun legacyConflict() = PendingSyncOperationEntity(
        ownerUserId = OWNER_ID,
        id = "legacy-operation",
        operationType = SyncOperationType.ADD_PLAYLIST_ENTRY,
        targetType = SyncTargetType.PLAYLIST,
        targetId = PLAYLIST_ID,
        requestPayloadJson = null,
        idempotencyKey = "legacy-key",
        status = SyncOperationStatus.CONFLICT,
        attemptCount = 1,
        createdAtEpochMs = 1,
        updatedAtEpochMs = 1,
        nextAttemptAtEpochMs = 1,
        leaseOwner = null,
        leaseExpiresAtEpochMs = null,
        lastErrorCode = "VERSION_CONFLICT",
    )

    private fun summary(version: Long, trackCount: Int) = PlaylistSummaryDto(
        id = PLAYLIST_ID,
        owner = OWNER,
        name = "Remote",
        description = null,
        visibility = "PRIVATE",
        cover = null,
        trackCount = trackCount,
        version = version,
        createdAt = NOW.toString(),
        updatedAt = NOW.toString(),
    )

    private fun detail(version: Long) = PlaylistDetailDto(
        id = PLAYLIST_ID,
        owner = OWNER,
        name = "Remote",
        description = null,
        visibility = "PRIVATE",
        cover = null,
        trackCount = 0,
        version = version,
        createdAt = NOW.toString(),
        updatedAt = NOW.toString(),
        entries = emptyList(),
        nextCursor = null,
    )

    private data object SignedInSessionProvider : AppSessionProvider {
        override val sessionState =
            MutableStateFlow<AppSessionState>(AppSessionState.SignedIn(OWNER_ID))

        override suspend fun restoreSession() = Unit
    }

    private class RefreshRemote(
        private val summaries: List<PlaylistSummaryDto> = emptyList(),
        private val detail: PlaylistDetailDto? = null,
    ) : PlaylistRemoteDataSource {
        override suspend fun allPlaylists(sort: String): List<PlaylistSummaryDto> = summaries

        override suspend fun playlist(playlistId: String): PlaylistDetailDto = requireNotNull(detail)

        override suspend fun create(
            idempotencyKey: String,
            request: CreatePlaylistRequestDto,
        ): PlaylistSummaryDto = error("unused")

        override suspend fun update(
            playlistId: String,
            idempotencyKey: String,
            payload: PlaylistUpdatePayload,
        ): PlaylistSummaryDto = error("unused")

        override suspend fun delete(
            playlistId: String,
            expectedVersion: Long,
            idempotencyKey: String,
        ) = error("unused")

        override suspend fun addTrack(
            playlistId: String,
            idempotencyKey: String,
            request: AddPlaylistTrackRequestDto,
        ): PlaylistEntryMutationDto = error("unused")

        override suspend fun removeTrack(
            playlistId: String,
            entryId: String,
            expectedVersion: Long,
            idempotencyKey: String,
        ): PlaylistMutationDto = error("unused")

        override suspend fun reorder(
            playlistId: String,
            idempotencyKey: String,
            request: ReorderPlaylistRequestDto,
        ): PlaylistMutationDto = error("unused")
    }

    private companion object {
        const val OWNER_ID = "00000000-0000-0000-0000-000000000001"
        const val PLAYLIST_ID = "00000000-0000-0000-0000-000000000002"
        const val ENTRY_ID = "00000000-0000-0000-0000-000000000003"
        const val TRACK_ID = "00000000-0000-0000-0000-000000000004"
        val OWNER = UserSummaryDto(OWNER_ID, "owner", "Owner", null)
        val NOW: Instant = Instant.parse("2026-01-01T00:00:00Z")
        val CLOCK: Clock = Clock.fixed(NOW, ZoneOffset.UTC)
    }
}
