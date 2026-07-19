package com.xymusic.app.data.sync

import android.app.Application
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.data.media.RoomCatalogLocalDataSource
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.entity.PendingSyncOperationEntity
import com.xymusic.app.core.database.model.SyncOperationStatus
import com.xymusic.app.core.database.model.SyncOperationType
import com.xymusic.app.core.database.model.SyncTargetType
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.feature.library.data.remote.FavoriteItemDto
import com.xymusic.app.feature.library.data.remote.HistoryItemDto
import com.xymusic.app.feature.library.data.remote.LibraryProtocolException
import com.xymusic.app.feature.library.data.remote.LibraryRemoteDataSource
import com.xymusic.app.feature.library.data.remote.RecordPlaybackRequestDto
import com.xymusic.app.feature.library.data.sync.FavoritePendingPayload
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import java.util.UUID
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import org.junit.After
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class PendingSyncOperationExecutorTest {
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
    fun deterministicLibraryProtocolFailureBecomesTerminalConflict() = runTest {
        val json = Json
        val trackId = UUID.randomUUID().toString()
        val operation =
            pendingOperation(
                operationType = SyncOperationType.ADD_FAVORITE,
                targetType = SyncTargetType.FAVORITE,
                targetId = trackId,
                requestPayloadJson = json.encodeToString(FavoritePendingPayload(trackId)),
            )

        assertThat(executor(json = json).execute(operation))
            .isEqualTo(PendingExecutionOutcome.Conflict("PROTOCOL_ERROR"))
    }

    @Test
    fun operationOwnedByAnotherUserStopsBeforeRemoteDispatch() = runTest {
        val trackId = UUID.randomUUID().toString()
        val operation =
            pendingOperation(
                operationType = SyncOperationType.ADD_FAVORITE,
                targetType = SyncTargetType.FAVORITE,
                targetId = trackId,
                requestPayloadJson = Json.encodeToString(FavoritePendingPayload(trackId)),
            )

        val outcome = executor(sessionProvider = DifferentOwnerSessionProvider).execute(operation)

        assertThat(outcome).isEqualTo(PendingExecutionOutcome.OwnerChanged)
    }

    @Test
    fun ownerChangingDuringRemoteCallStopsBeforeLocalMutation() = runTest {
        val sessionProvider = MutableSessionProvider(OWNER_ID)
        val trackId = UUID.randomUUID().toString()
        val operation =
            pendingOperation(
                operationType = SyncOperationType.REMOVE_FAVORITE,
                targetType = SyncTargetType.FAVORITE,
                targetId = trackId,
                requestPayloadJson = Json.encodeToString(FavoritePendingPayload(trackId)),
            )
        val remote =
            object : LibraryRemoteDataSource by ProtocolFailingLibraryRemote {
                override suspend fun removeFavorite(trackId: String) {
                    sessionProvider.sessionState.value =
                        AppSessionState.SignedIn(
                            UUID.randomUUID().toString(),
                        )
                }
            }

        val outcome =
            executor(
                libraryRemote = remote,
                sessionProvider = sessionProvider,
            ).execute(operation)

        assertThat(outcome).isEqualTo(PendingExecutionOutcome.OwnerChanged)
    }

    @Test
    fun malformedPayloadBecomesTerminalConflictWithoutRemoteDispatch() = runTest {
        val operation =
            pendingOperation(
                operationType = SyncOperationType.ADD_FAVORITE,
                targetType = SyncTargetType.FAVORITE,
                targetId = UUID.randomUUID().toString(),
                requestPayloadJson = "{",
            )

        val outcome = executor().execute(operation)

        assertThat(outcome)
            .isEqualTo(PendingExecutionOutcome.Conflict("INVALID_PENDING_PAYLOAD"))
    }

    @Test
    fun legacyPlaylistOperationIsDiscardedWithoutRemoteDispatch() = runTest {
        val operation =
            pendingOperation(
                operationType = SyncOperationType.ADD_PLAYLIST_ENTRY,
                targetType = SyncTargetType.PLAYLIST,
                targetId = UUID.randomUUID().toString(),
                requestPayloadJson = "not-needed",
            )

        assertThat(executor().execute(operation)).isEqualTo(PendingExecutionOutcome.Success)
    }

    private fun executor(
        libraryRemote: LibraryRemoteDataSource = ProtocolFailingLibraryRemote,
        sessionProvider: AppSessionProvider = SignedInSessionProvider,
        json: Json = Json,
    ) = PendingSyncOperationExecutor(
        database = database,
        libraryDao = database.libraryDao(),
        pendingDao = database.pendingSyncOperationDao(),
        catalogLocal = RoomCatalogLocalDataSource(database.catalogDao()),
        libraryRemote = libraryRemote,
        sessionProvider = sessionProvider,
        sessionMutationCoordinator = SessionMutationCoordinator(),
        json = json,
        clock = Clock.fixed(Instant.EPOCH, ZoneOffset.UTC),
    )

    private fun pendingOperation(
        operationType: SyncOperationType,
        targetType: SyncTargetType,
        targetId: String?,
        requestPayloadJson: String?,
        status: SyncOperationStatus = SyncOperationStatus.RUNNING,
        createdAtEpochMs: Long = 1,
    ) = PendingSyncOperationEntity(
        ownerUserId = OWNER_ID,
        id = UUID.randomUUID().toString(),
        operationType = operationType,
        targetType = targetType,
        targetId = targetId,
        requestPayloadJson = requestPayloadJson,
        idempotencyKey = UUID.randomUUID().toString(),
        status = status,
        attemptCount = if (status == SyncOperationStatus.RUNNING) 1 else 0,
        createdAtEpochMs = createdAtEpochMs,
        updatedAtEpochMs = createdAtEpochMs,
        nextAttemptAtEpochMs = createdAtEpochMs,
        leaseOwner = if (status == SyncOperationStatus.RUNNING) "worker" else null,
        leaseExpiresAtEpochMs = if (status == SyncOperationStatus.RUNNING) 2 else null,
        lastErrorCode = null,
    )

    private data object SignedInSessionProvider : AppSessionProvider {
        override val sessionState = MutableStateFlow<AppSessionState>(AppSessionState.SignedIn(OWNER_ID))

        override suspend fun restoreSession() = Unit
    }

    private data object DifferentOwnerSessionProvider : AppSessionProvider {
        override val sessionState =
            MutableStateFlow<AppSessionState>(
                AppSessionState.SignedIn(UUID.randomUUID().toString()),
            )

        override suspend fun restoreSession() = Unit
    }

    private class MutableSessionProvider(ownerUserId: String) : AppSessionProvider {
        override val sessionState =
            MutableStateFlow<AppSessionState>(
                AppSessionState.SignedIn(ownerUserId),
            )

        override suspend fun restoreSession() = Unit
    }

    private data object ProtocolFailingLibraryRemote : LibraryRemoteDataSource {
        override suspend fun allFavorites(sort: String): List<FavoriteItemDto> = error("unused")

        override suspend fun addFavorite(trackId: String): FavoriteItemDto =
            throw LibraryProtocolException("Malformed favorite")

        override suspend fun removeFavorite(trackId: String) = error("unused")

        override fun historyPages(): kotlinx.coroutines.flow.Flow<List<HistoryItemDto>> = error("unused")

        override suspend fun recordPlayback(
            trackId: String,
            idempotencyKey: String,
            request: RecordPlaybackRequestDto,
        ): HistoryItemDto = error("unused")
    }

    private companion object {
        val OWNER_ID: String = UUID.randomUUID().toString()
    }
}
