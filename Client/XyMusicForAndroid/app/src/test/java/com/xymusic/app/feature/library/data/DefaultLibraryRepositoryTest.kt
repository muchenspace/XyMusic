package com.xymusic.app.feature.library.data

import android.app.Application
import androidx.paging.PagingSource
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.data.media.RoomCatalogLocalDataSource
import com.xymusic.app.core.data.media.remote.ArtistReferenceDto
import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.entity.FavoriteEntity
import com.xymusic.app.core.database.entity.PendingSyncOperationEntity
import com.xymusic.app.core.database.model.PlaybackHistoryReadModel
import com.xymusic.app.core.database.model.SyncOperationStatus
import com.xymusic.app.core.database.model.SyncOperationType
import com.xymusic.app.core.database.model.SyncTargetType
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.core.sync.PendingSyncScheduler
import com.xymusic.app.feature.library.data.remote.FavoriteItemDto
import com.xymusic.app.feature.library.data.remote.HistoryItemDto
import com.xymusic.app.feature.library.data.remote.LibraryRemoteDataSource
import com.xymusic.app.feature.library.data.remote.RecordPlaybackRequestDto
import com.xymusic.app.feature.library.domain.LibraryResult
import com.xymusic.app.feature.library.domain.model.PlaybackEvent
import com.xymusic.app.feature.library.domain.model.PlaybackProgressCommand
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import java.util.UUID
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import org.junit.After
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
@OptIn(ExperimentalCoroutinesApi::class)
class DefaultLibraryRepositoryTest {
    private lateinit var database: XyMusicDatabase
    private val remote = FakeLibraryRemoteDataSource()
    private val scheduler = RecordingScheduler()
    private val sessionProvider = SignedInSessionProvider(OWNER_ID)
    private val mutationCoordinator = SessionMutationCoordinator()
    private val serverRuntimeCoordinator = ServerRuntimeCoordinator()
    private val clock = Clock.fixed(Instant.parse("2026-07-11T00:00:00Z"), ZoneOffset.UTC)

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
    fun fullFavoriteRefreshRemovesStaleRowsAndReappliesPendingIntent() = runTest {
        seedTrack(STALE_TRACK_ID)
        seedTrack(PENDING_ADD_TRACK_ID)
        seedTrack(PENDING_REMOVE_TRACK_ID)
        database.libraryDao().upsertFavorite(FavoriteEntity(OWNER_ID, STALE_TRACK_ID, 1))
        database.libraryDao().upsertFavorite(FavoriteEntity(OWNER_ID, PENDING_ADD_TRACK_ID, 2))
        enqueueFavorite(PENDING_ADD_TRACK_ID, SyncOperationType.ADD_FAVORITE, 3)
        enqueueFavorite(PENDING_REMOVE_TRACK_ID, SyncOperationType.REMOVE_FAVORITE, 4)
        enqueueFavorite(UNCACHED_PENDING_ADD_TRACK_ID, SyncOperationType.ADD_FAVORITE, 5)
        remote.favorites = listOf(favorite(PENDING_REMOVE_TRACK_ID))

        val result = repository().refreshFavorites()

        assertThat(result).isEqualTo(LibraryResult.Success(Unit))
        assertThat(database.libraryDao().observeFavorite(OWNER_ID, STALE_TRACK_ID).first()).isFalse()
        assertThat(database.libraryDao().observeFavorite(OWNER_ID, PENDING_ADD_TRACK_ID).first()).isTrue()
        assertThat(database.libraryDao().observeFavorite(OWNER_ID, PENDING_REMOVE_TRACK_ID).first()).isFalse()
        assertThat(
            database.libraryDao().observeFavorite(OWNER_ID, UNCACHED_PENDING_ADD_TRACK_ID).first(),
        ).isFalse()
    }

    @Test
    fun staleFavoriteRefreshDoesNotDeleteFavoriteAddedWhileRequestIsInFlight() = runTest {
        val trackId = UUID.randomUUID().toString()
        seedTrack(trackId)
        val requestStarted = CompletableDeferred<Unit>()
        val releaseResponse = CompletableDeferred<Unit>()
        remote.favoriteLoader = {
            requestStarted.complete(Unit)
            releaseResponse.await()
            emptyList()
        }
        remote.addFavoriteLoader = { favorite(it) }
        val repository = repository()

        val refresh = async { repository.refreshFavorites() }
        requestStarted.await()
        assertThat(repository.setFavorite(trackId, true)).isEqualTo(LibraryResult.Success(Unit))
        releaseResponse.complete(Unit)

        assertThat(refresh.await()).isEqualTo(LibraryResult.Success(Unit))
        assertThat(database.libraryDao().favorite(OWNER_ID, trackId)).isNotNull()
    }

    @Test
    fun staleFavoriteRefreshDoesNotRestoreFavoriteRemovedWhileRequestIsInFlight() = runTest {
        val trackId = UUID.randomUUID().toString()
        seedTrack(trackId)
        database.libraryDao().upsertFavorite(FavoriteEntity(OWNER_ID, trackId, 1L))
        val requestStarted = CompletableDeferred<Unit>()
        val releaseResponse = CompletableDeferred<Unit>()
        remote.favoriteLoader = {
            requestStarted.complete(Unit)
            releaseResponse.await()
            listOf(favorite(trackId))
        }
        remote.removeFavoriteLoader = { Unit }
        val repository = repository()

        val refresh = async { repository.refreshFavorites() }
        requestStarted.await()
        assertThat(repository.setFavorite(trackId, false)).isEqualTo(LibraryResult.Success(Unit))
        releaseResponse.complete(Unit)

        assertThat(refresh.await()).isEqualTo(LibraryResult.Success(Unit))
        assertThat(database.libraryDao().favorite(OWNER_ID, trackId)).isNull()
    }

    @Test
    fun rapidReverseFavoriteOperationsForOneTrackAreSerialized() = runTest {
        val trackId = UUID.randomUUID().toString()
        seedTrack(trackId)
        val addStarted = CompletableDeferred<Unit>()
        val releaseAdd = CompletableDeferred<Unit>()
        val calls = mutableListOf<String>()
        remote.addFavoriteLoader = {
            calls += "add"
            addStarted.complete(Unit)
            releaseAdd.await()
            favorite(it)
        }
        remote.removeFavoriteLoader = {
            calls += "remove"
        }
        val repository = repository()

        val add = async { repository.setFavorite(trackId, true) }
        addStarted.await()
        val remove = async { repository.setFavorite(trackId, false) }
        runCurrent()

        assertThat(calls).containsExactly("add")
        releaseAdd.complete(Unit)
        assertThat(add.await()).isEqualTo(LibraryResult.Success(Unit))
        assertThat(remove.await()).isEqualTo(LibraryResult.Success(Unit))
        assertThat(calls).containsExactly("add", "remove").inOrder()
        assertThat(database.libraryDao().favorite(OWNER_ID, trackId)).isNull()
    }

    @Test
    fun successfulRemoveAfterPendingAddAppendsStrictlyLaterRemoveIntent() = runTest {
        val trackId = UUID.randomUUID().toString()
        seedTrack(trackId)
        database.libraryDao().upsertFavorite(FavoriteEntity(OWNER_ID, trackId, 1L))
        val running =
            enqueueFavorite(
                trackId = trackId,
                type = SyncOperationType.ADD_FAVORITE,
                time = clock.millis() - 1L,
                status = SyncOperationStatus.RUNNING,
            )
        val old =
            enqueueFavorite(
                trackId = trackId,
                type = SyncOperationType.ADD_FAVORITE,
                time = clock.millis(),
            )
        remote.removeFavoriteLoader = { Unit }
        val repository = repository()

        assertThat(repository.setFavorite(trackId, false)).isEqualTo(LibraryResult.Success(Unit))

        val operations =
            database
                .pendingSyncOperationDao()
                .forTarget(OWNER_ID, SyncTargetType.FAVORITE, trackId)
        assertThat(operations.map(PendingSyncOperationEntity::operationType))
            .containsExactly(
                SyncOperationType.ADD_FAVORITE,
                SyncOperationType.ADD_FAVORITE,
                SyncOperationType.REMOVE_FAVORITE,
            ).inOrder()
        assertThat(operations.first().id).isEqualTo(running.id)
        assertThat(operations.first().status).isEqualTo(SyncOperationStatus.RUNNING)
        assertThat(operations[1].id).isEqualTo(old.id)
        assertThat(operations.last().createdAtEpochMs).isGreaterThan(old.createdAtEpochMs)
        assertThat(database.libraryDao().favorite(OWNER_ID, trackId)).isNull()
    }

    @Test
    fun successfulAddAfterPendingRemoveAppendsStrictlyLaterAddIntent() = runTest {
        val trackId = UUID.randomUUID().toString()
        seedTrack(trackId)
        val old =
            enqueueFavorite(
                trackId = trackId,
                type = SyncOperationType.REMOVE_FAVORITE,
                time = clock.millis(),
            )
        remote.addFavoriteLoader = { favorite(it) }
        val repository = repository()

        assertThat(repository.setFavorite(trackId, true)).isEqualTo(LibraryResult.Success(Unit))

        val operations =
            database
                .pendingSyncOperationDao()
                .forTarget(OWNER_ID, SyncTargetType.FAVORITE, trackId)
        assertThat(operations.map(PendingSyncOperationEntity::operationType))
            .containsExactly(
                SyncOperationType.REMOVE_FAVORITE,
                SyncOperationType.ADD_FAVORITE,
            ).inOrder()
        assertThat(operations.last().createdAtEpochMs).isGreaterThan(old.createdAtEpochMs)
        assertThat(database.libraryDao().favorite(OWNER_ID, trackId)).isNotNull()
    }

    @Test
    fun cancelledFavoriteMutationPersistsLatestIntentBeforeCompletingCancellation() = runTest {
        val trackId = UUID.randomUUID().toString()
        seedTrack(trackId)
        val addStarted = CompletableDeferred<Unit>()
        val neverComplete = CompletableDeferred<Unit>()
        remote.addFavoriteLoader = {
            addStarted.complete(Unit)
            neverComplete.await()
            favorite(it)
        }
        val repository = repository()

        val mutation = launch { repository.setFavorite(trackId, true) }
        addStarted.await()
        mutation.cancelAndJoin()

        assertThat(database.libraryDao().favorite(OWNER_ID, trackId)).isNotNull()
        val pending = database.pendingSyncOperationDao().observeAll(OWNER_ID).first()
        assertThat(pending).hasSize(1)
        assertThat(pending.single().operationType).isEqualTo(SyncOperationType.ADD_FAVORITE)
        assertThat(pending.single().targetId).isEqualTo(trackId)
        assertThat(scheduler.scheduledOwners).containsExactly(OWNER_ID)
    }

    @Test
    fun oldServerFavoriteResponseCannotRepopulateClearedData() = runTest {
        val trackId = UUID.randomUUID().toString()
        seedTrack(trackId)
        val addStarted = CompletableDeferred<Unit>()
        val releaseAdd = CompletableDeferred<Unit>()
        remote.addFavoriteLoader = {
            addStarted.complete(Unit)
            releaseAdd.await()
            favorite(it)
        }
        val repository = repository()

        val mutation = async { repository.setFavorite(trackId, true) }
        addStarted.await()
        val switchGeneration = serverRuntimeCoordinator.beginSwitch()
        database.clearAllTables()
        serverRuntimeCoordinator.finishSwitch(switchGeneration)
        releaseAdd.complete(Unit)

        assertThat(mutation.await()).isInstanceOf(LibraryResult.Failure::class.java)
        assertThat(database.libraryDao().favorite(OWNER_ID, trackId)).isNull()
        assertThat(database.pendingSyncOperationDao().observeAll(OWNER_ID).first()).isEmpty()
    }

    @Test
    fun playbackCheckpointIsPersistedBeforeAnyNetworkAttempt() = runTest {
        seedTrack(PLAYBACK_TRACK_ID)
        val sessionId = UUID.randomUUID().toString()

        val result =
            repository().recordPlayback(
                PlaybackProgressCommand(
                    trackId = PLAYBACK_TRACK_ID,
                    playbackSessionId = sessionId,
                    positionMs = 12_345,
                    occurredAtEpochMillis = clock.millis(),
                    event = PlaybackEvent.STARTED,
                ),
            )

        assertThat(result).isEqualTo(LibraryResult.Success(Unit))
        assertThat(remote.recordPlaybackCalls).isEqualTo(0)
        assertThat(database.libraryDao().history(OWNER_ID, PLAYBACK_TRACK_ID)?.lastPositionMs)
            .isEqualTo(12_345)
        val pending = database.pendingSyncOperationDao().observeAll(OWNER_ID).first()
        assertThat(pending).hasSize(1)
        assertThat(pending.single().operationType).isEqualTo(SyncOperationType.RECORD_PLAYBACK)
        assertThat(scheduler.scheduledOwners).containsExactly(OWNER_ID)
    }

    @Test
    fun consecutiveUnattemptedProgressCheckpointsKeepOnlyTheLatestPendingPayload() = runTest {
        seedTrack(PLAYBACK_TRACK_ID)
        val sessionId = UUID.randomUUID().toString()

        repository().recordPlayback(
            PlaybackProgressCommand(
                PLAYBACK_TRACK_ID,
                sessionId,
                10_000,
                clock.millis(),
                PlaybackEvent.PROGRESS,
            ),
        )
        repository().recordPlayback(
            PlaybackProgressCommand(
                PLAYBACK_TRACK_ID,
                sessionId,
                25_000,
                clock.millis() + 1_000,
                PlaybackEvent.PROGRESS,
            ),
        )

        val pending = database.pendingSyncOperationDao().observeAll(OWNER_ID).first()
        assertThat(pending).hasSize(1)
        assertThat(pending.single().requestPayloadJson).contains("\"positionMs\":25000")
        assertThat(database.libraryDao().history(OWNER_ID, PLAYBACK_TRACK_ID)?.lastPositionMs)
            .isEqualTo(25_000)
    }

    @Test
    fun historyRefreshMergesOlderServerSnapshotWithPendingPlayback() = runTest {
        seedTrack(PLAYBACK_TRACK_ID)
        val sessionId = UUID.randomUUID().toString()
        repository().recordPlayback(
            PlaybackProgressCommand(
                trackId = PLAYBACK_TRACK_ID,
                playbackSessionId = sessionId,
                positionMs = 12_345,
                occurredAtEpochMillis = clock.millis(),
                event = PlaybackEvent.STARTED,
            ),
        )
        remote.historyItems =
            listOf(
                historyItem(
                    trackId = PLAYBACK_TRACK_ID,
                    positionMs = 1_000,
                    playCount = 7,
                    updatedAtEpochMillis = clock.millis() - 1_000,
                ),
            )

        val result = repository().refreshHistory()

        assertThat(result).isEqualTo(LibraryResult.Success(Unit))
        val history = database.libraryDao().history(OWNER_ID, PLAYBACK_TRACK_ID)
        assertThat(history?.lastPositionMs).isEqualTo(12_345)
        assertThat(history?.playCount).isEqualTo(8)
        assertThat(history?.updatedAtEpochMs).isEqualTo(clock.millis())
    }

    @Test
    fun historyRefreshPersistsEachPageBeforeLoadingTheNextPage() = runTest {
        val firstTrackId = UUID.randomUUID().toString()
        val secondTrackId = UUID.randomUUID().toString()
        remote.historyPageLoader = {
            flow {
                emit(
                    listOf(
                        historyItem(firstTrackId, 1_000, 1, clock.millis() - 2_000),
                    ),
                )
                assertThat(database.libraryDao().history(OWNER_ID, firstTrackId)).isNotNull()
                emit(
                    listOf(
                        historyItem(secondTrackId, 2_000, 2, clock.millis() - 1_000),
                    ),
                )
            }
        }

        val result = repository().refreshHistory()

        assertThat(result).isEqualTo(LibraryResult.Success(Unit))
        assertThat(database.libraryDao().history(OWNER_ID, firstTrackId)).isNotNull()
        assertThat(database.libraryDao().history(OWNER_ID, secondTrackId)).isNotNull()
    }

    @Test
    fun historyRefreshCannotWriteLaterPageAfterSessionCleanup() = runTest {
        val firstTrackId = UUID.randomUUID().toString()
        val secondTrackId = UUID.randomUUID().toString()
        val firstPagePersisted = CompletableDeferred<Unit>()
        val releaseSecondPage = CompletableDeferred<Unit>()
        remote.historyPageLoader = {
            flow {
                emit(listOf(historyItem(firstTrackId, 1_000, 1, clock.millis() - 2_000)))
                firstPagePersisted.complete(Unit)
                releaseSecondPage.await()
                emit(listOf(historyItem(secondTrackId, 2_000, 2, clock.millis() - 1_000)))
            }
        }
        val refresh = async { repository().refreshHistory() }
        firstPagePersisted.await()

        mutationCoordinator.mutate {
            sessionProvider.signOut()
            database.accountDataDao().deletePrivateData(OWNER_ID)
        }
        releaseSecondPage.complete(Unit)

        assertThat(refresh.await()).isInstanceOf(LibraryResult.Failure::class.java)
        assertThat(database.libraryDao().history(OWNER_ID, firstTrackId)).isNull()
        assertThat(database.libraryDao().history(OWNER_ID, secondTrackId)).isNull()
    }

    @Test
    fun historyPagingSourceReturnsNewestTracksFirst() = runTest {
        val olderTrackId = UUID.randomUUID().toString()
        seedTrack(olderTrackId)
        seedTrack(PLAYBACK_TRACK_ID)
        database.libraryDao().upsertHistory(history(olderTrackId, lastPlayedAt = 1_000))
        database.libraryDao().upsertHistory(history(PLAYBACK_TRACK_ID, lastPlayedAt = 2_000))

        val result =
            database.libraryDao().pagedHistory(OWNER_ID).load(
                PagingSource.LoadParams.Refresh(key = null, loadSize = 20, placeholdersEnabled = false),
            ) as PagingSource.LoadResult.Page<Int, PlaybackHistoryReadModel>

        assertThat(result.data.map { it.track.id })
            .containsExactly(PLAYBACK_TRACK_ID, olderTrackId)
            .inOrder()
    }

    @Test
    fun refreshCannotReinsertPrivateRowsAfterSessionCleanup() = runTest {
        val requestStarted = CompletableDeferred<Unit>()
        val releaseResponse = CompletableDeferred<Unit>()
        remote.favoriteLoader = {
            requestStarted.complete(Unit)
            releaseResponse.await()
            listOf(favorite(PENDING_ADD_TRACK_ID))
        }
        val refresh = async { repository().refreshFavorites() }
        requestStarted.await()

        mutationCoordinator.mutate {
            sessionProvider.signOut()
            database.accountDataDao().deletePrivateData(OWNER_ID)
        }
        releaseResponse.complete(Unit)

        assertThat(refresh.await()).isInstanceOf(LibraryResult.Failure::class.java)
        assertThat(database.libraryDao().observeFavorite(OWNER_ID, PENDING_ADD_TRACK_ID).first())
            .isFalse()
    }

    private fun repository() = DefaultLibraryRepository(
        database = database,
        libraryDao = database.libraryDao(),
        catalogDao = database.catalogDao(),
        pendingDao = database.pendingSyncOperationDao(),
        catalogLocal = RoomCatalogLocalDataSource(database.catalogDao()),
        remote = remote,
        sessionProvider = sessionProvider,
        sessionMutationCoordinator = mutationCoordinator,
        serverRuntimeCoordinator = serverRuntimeCoordinator,
        pendingSyncScheduler = scheduler,
        json = Json,
        clock = clock,
        ioDispatcher = UnconfinedTestDispatcher(),
    )

    private suspend fun seedTrack(trackId: String) {
        RoomCatalogLocalDataSource(database.catalogDao()).mergeTrackSummaries(
            listOf(track(trackId)),
            clock.millis(),
        )
    }

    private suspend fun enqueueFavorite(
        trackId: String,
        type: SyncOperationType,
        time: Long,
        status: SyncOperationStatus = SyncOperationStatus.PENDING,
    ): PendingSyncOperationEntity {
        val operation =
            PendingSyncOperationEntity(
                ownerUserId = OWNER_ID,
                id = UUID.randomUUID().toString(),
                operationType = type,
                targetType = SyncTargetType.FAVORITE,
                targetId = trackId,
                requestPayloadJson = "{\"trackId\":\"$trackId\"}",
                idempotencyKey = UUID.randomUUID().toString(),
                status = status,
                attemptCount = if (status == SyncOperationStatus.RUNNING) 1 else 0,
                createdAtEpochMs = time,
                updatedAtEpochMs = time,
                nextAttemptAtEpochMs = time,
                leaseOwner = if (status == SyncOperationStatus.RUNNING) "worker" else null,
                leaseExpiresAtEpochMs = if (status == SyncOperationStatus.RUNNING) time + 60_000 else null,
                lastErrorCode = null,
            )
        database.pendingSyncOperationDao().enqueue(operation)
        return operation
    }

    private fun history(trackId: String, lastPlayedAt: Long) = com.xymusic.app.core.database.entity.HistoryEntity(
        ownerUserId = OWNER_ID,
        trackId = trackId,
        lastPositionMs = 0,
        playCount = 1,
        lastPlayedAtEpochMs = lastPlayedAt,
        completed = false,
        updatedAtEpochMs = lastPlayedAt,
    )

    private fun favorite(trackId: String) = FavoriteItemDto(
        track = track(trackId),
        favoritedAt = "2026-07-11T00:00:00Z",
    )

    private fun historyItem(trackId: String, positionMs: Long, playCount: Long, updatedAtEpochMillis: Long) =
        HistoryItemDto(
            track = track(trackId),
            lastPositionMs = positionMs,
            playCount = playCount,
            lastPlayedAt = Instant.ofEpochMilli(updatedAtEpochMillis).toString(),
            completed = false,
            updatedAt = Instant.ofEpochMilli(updatedAtEpochMillis).toString(),
        )

    private fun track(trackId: String) = TrackSummaryDto(
        id = trackId,
        title = "Track $trackId",
        artists = listOf(ArtistReferenceDto(ARTIST_ID, "Artist")),
        album = null,
        artwork = null,
        durationMs = 180_000,
        trackNumber = 1,
        discNumber = 1,
        isFavorite = false,
        publishedAt = "2026-07-10T00:00:00Z",
    )

    private class SignedInSessionProvider(ownerUserId: String) : AppSessionProvider {
        override val sessionState =
            MutableStateFlow<AppSessionState>(
                AppSessionState.SignedIn(ownerUserId),
            )

        override suspend fun restoreSession() = Unit

        fun signOut() {
            sessionState.value = AppSessionState.SignedOut
        }
    }

    private class RecordingScheduler : PendingSyncScheduler {
        val scheduledOwners = mutableListOf<String>()

        override fun schedule(ownerUserId: String) {
            scheduledOwners += ownerUserId
        }

        override fun cancel(ownerUserId: String) = Unit
    }

    private class FakeLibraryRemoteDataSource : LibraryRemoteDataSource {
        var favorites: List<FavoriteItemDto> = emptyList()
        var historyItems: List<HistoryItemDto> = emptyList()
        var recordPlaybackCalls = 0
        var favoriteLoader: suspend () -> List<FavoriteItemDto> = { favorites }
        var addFavoriteLoader: suspend (String) -> FavoriteItemDto = { error("unused") }
        var removeFavoriteLoader: suspend (String) -> Unit = { error("unused") }

        override suspend fun allFavorites(sort: String): List<FavoriteItemDto> = favoriteLoader()

        override suspend fun addFavorite(trackId: String): FavoriteItemDto = addFavoriteLoader(trackId)

        override suspend fun removeFavorite(trackId: String) = removeFavoriteLoader(trackId)

        var historyPageLoader: () -> Flow<List<HistoryItemDto>> = { flowOf(historyItems) }

        override fun historyPages(): Flow<List<HistoryItemDto>> = historyPageLoader()

        override suspend fun recordPlayback(
            trackId: String,
            idempotencyKey: String,
            request: RecordPlaybackRequestDto,
        ): HistoryItemDto {
            recordPlaybackCalls += 1
            error("Playback must be queued before network delivery")
        }
    }

    private companion object {
        val OWNER_ID: String = UUID.randomUUID().toString()
        val ARTIST_ID: String = UUID.randomUUID().toString()
        val STALE_TRACK_ID: String = UUID.randomUUID().toString()
        val PENDING_ADD_TRACK_ID: String = UUID.randomUUID().toString()
        val PENDING_REMOVE_TRACK_ID: String = UUID.randomUUID().toString()
        val UNCACHED_PENDING_ADD_TRACK_ID: String = UUID.randomUUID().toString()
        val PLAYBACK_TRACK_ID: String = UUID.randomUUID().toString()
    }
}
