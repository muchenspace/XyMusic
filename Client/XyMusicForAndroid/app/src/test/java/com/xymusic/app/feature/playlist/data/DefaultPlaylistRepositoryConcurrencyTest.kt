package com.xymusic.app.feature.playlist.data

import android.app.Application
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.data.media.RoomCatalogLocalDataSource
import com.xymusic.app.core.data.media.remote.ArtistReferenceDto
import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.entity.PlaylistEntity
import com.xymusic.app.core.database.entity.PlaylistEntryEntity
import com.xymusic.app.core.database.entity.TrackEntity
import com.xymusic.app.core.database.model.PlaylistVisibility as StoredPlaylistVisibility
import com.xymusic.app.core.database.model.SyncTargetType
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.network.model.ProblemCode
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.feature.playlist.data.remote.AddPlaylistTrackRequestDto
import com.xymusic.app.feature.playlist.data.remote.CreatePlaylistRequestDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistDetailDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistEntryDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistEntryMutationDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistMutationDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistRemoteDataSource
import com.xymusic.app.feature.playlist.data.remote.PlaylistRemoteException
import com.xymusic.app.feature.playlist.data.remote.PlaylistSummaryDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistUpdatePayload
import com.xymusic.app.feature.playlist.data.remote.ReorderPlaylistRequestDto
import com.xymusic.app.feature.playlist.data.remote.UserSummaryDto
import com.xymusic.app.feature.playlist.domain.PlaylistResult
import com.xymusic.app.feature.playlist.domain.model.AddPlaylistTrackCommand
import com.xymusic.app.feature.playlist.domain.model.PlaylistVersionConflict
import com.xymusic.app.feature.playlist.domain.model.RemovePlaylistTrackCommand
import com.xymusic.app.feature.playlist.domain.model.ReorderPlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.UpdatePlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.ValueChange
import java.io.IOException
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import java.util.UUID
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.Executor
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runCurrent
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
class DefaultPlaylistRepositoryConcurrencyTest {
    private lateinit var database: XyMusicDatabase
    private val executedQueries = CopyOnWriteArrayList<String>()

    @Before
    fun setUp() {
        database =
            Room
                .inMemoryDatabaseBuilder(
                    ApplicationProvider.getApplicationContext(),
                    XyMusicDatabase::class.java,
                ).allowMainThreadQueries()
                .setQueryCallback(
                    { query, _ -> executedQueries += query },
                    Executor { command -> command.run() },
                ).build()
    }

    @After
    fun tearDown() {
        database.close()
    }

    @Test
    fun concurrentMutationsOfOnePlaylistDoNotReuseTheSameVersion() = runTest {
        database.playlistDao().upsertPlaylist(
            playlistEntity(name = "Original", version = 1),
        )
        val remote = BlockingUpdateRemote()
        val repository = repository(remote, UnconfinedTestDispatcher(testScheduler))

        val first =
            async {
                repository.update(
                    UpdatePlaylistCommand(PLAYLIST_ID, 1, name = ValueChange.Set("First")),
                )
            }
        remote.updateStarted.await()
        val second =
            async {
                repository.update(
                    UpdatePlaylistCommand(PLAYLIST_ID, 1, name = ValueChange.Set("Second")),
                )
            }
        runCurrent()

        assertThat(remote.updateCalls).isEqualTo(1)
        remote.releaseUpdate.complete(Unit)

        assertThat(first.await()).isInstanceOf(PlaylistResult.Success::class.java)
        assertThat(second.await()).isInstanceOf(PlaylistResult.Conflict::class.java)
        assertThat(remote.updateCalls).isEqualTo(1)
        assertThat(database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)?.version).isEqualTo(2)
    }

    @Test
    fun metadataUpdateDoesNotReadPlaylistEntries() = runTest {
        database.playlistDao().upsertPlaylist(playlistEntity(name = "Original", version = 1))
        val remote = BlockingUpdateRemote().apply { releaseUpdate.complete(Unit) }
        val repository = repository(remote, UnconfinedTestDispatcher(testScheduler))
        executedQueries.clear()

        val result =
            repository.update(
                UpdatePlaylistCommand(PLAYLIST_ID, 1, name = ValueChange.Set("Updated")),
            )

        assertThat(result).isInstanceOf(PlaylistResult.Success::class.java)
        assertThat(
            executedQueries.none { query ->
                query.contains("playlist_entries", ignoreCase = true)
            },
        ).isTrue()
    }

    @Test
    fun cancelledUpdateKeepsCachedMetadataAndDoesNotQueueRetry() = runTest {
        database.playlistDao().upsertPlaylist(playlistEntity(name = "Original", version = 1))
        val remote = BlockingUpdateRemote()
        val repository = repository(remote, UnconfinedTestDispatcher(testScheduler))

        val mutation =
            async {
                repository.update(
                    UpdatePlaylistCommand(PLAYLIST_ID, 1, name = ValueChange.Set("Updated")),
                )
            }
        remote.updateStarted.await()
        mutation.cancelAndJoin()

        assertThat(mutation.isCancelled).isTrue()
        val stored = database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)
        assertThat(stored?.name).isEqualTo("Original")
        assertThat(stored?.version).isEqualTo(1)
        assertThat(pendingForPlaylist()).isEmpty()
    }

    @Test
    fun staleListRefreshDoesNotOverwriteNewerLocalVersion() = runTest {
        database.playlistDao().upsertPlaylist(playlistEntity(name = "Original", version = 1))
        val remote = BlockingListRemote(listOf(summary(name = "Stale", version = 1)))
        val repository = repository(remote, UnconfinedTestDispatcher(testScheduler))

        val refresh = async { repository.refreshPlaylists() }
        remote.requestStarted.await()
        database.playlistDao().upsertPlaylist(playlistEntity(name = "Newer", version = 2))
        remote.releaseRequest.complete(Unit)

        assertThat(refresh.await()).isInstanceOf(PlaylistResult.Success::class.java)
        val stored = database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)
        assertThat(stored?.name).isEqualTo("Newer")
        assertThat(stored?.version).isEqualTo(2)
    }

    @Test
    fun listRefreshDoesNotDeletePlaylistCreatedWhileRequestIsInFlight() = runTest {
        val remote = BlockingListRemote(emptyList())
        val repository = repository(remote, UnconfinedTestDispatcher(testScheduler))

        val refresh = async { repository.refreshPlaylists() }
        remote.requestStarted.await()
        database.playlistDao().upsertPlaylist(playlistEntity(name = "New", version = 1))
        remote.releaseRequest.complete(Unit)

        assertThat(refresh.await()).isInstanceOf(PlaylistResult.Success::class.java)
        assertThat(database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)).isNotNull()
    }

    @Test
    fun listRefreshDoesNotDeletePlaylistUpdatedWhileRequestIsInFlight() = runTest {
        database.playlistDao().upsertPlaylist(playlistEntity(name = "Original", version = 1))
        val remote = BlockingListRemote(emptyList())
        val repository = repository(remote, UnconfinedTestDispatcher(testScheduler))

        val refresh = async { repository.refreshPlaylists() }
        remote.requestStarted.await()
        database.playlistDao().upsertPlaylist(playlistEntity(name = "Updated", version = 2))
        remote.releaseRequest.complete(Unit)

        assertThat(refresh.await()).isInstanceOf(PlaylistResult.Success::class.java)
        val stored = database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)
        assertThat(stored?.name).isEqualTo("Updated")
        assertThat(stored?.version).isEqualTo(2)
    }

    @Test
    fun listRefreshDoesNotRestorePlaylistDeletedWhileRequestIsInFlight() = runTest {
        database.playlistDao().upsertPlaylist(playlistEntity(name = "Original", version = 1))
        val remote = BlockingListRemote(listOf(summary(name = "Original", version = 1)))
        val repository = repository(remote, UnconfinedTestDispatcher(testScheduler))

        val refresh = async { repository.refreshPlaylists() }
        remote.requestStarted.await()
        database.playlistDao().deletePlaylist(OWNER_ID, PLAYLIST_ID)
        remote.releaseRequest.complete(Unit)

        assertThat(refresh.await()).isInstanceOf(PlaylistResult.Success::class.java)
        assertThat(database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)).isNull()
    }

    @Test
    fun failedProgressiveRefreshKeepsExistingCompleteEntries() = runTest {
        database.catalogDao().upsertTrack(track(OLD_TRACK_ID))
        database.playlistDao().replacePlaylist(
            playlistEntity(name = "Cached", version = 1, trackCount = 1),
            listOf(
                PlaylistEntryEntity(
                    ownerUserId = OWNER_ID,
                    id = OLD_ENTRY_ID,
                    playlistId = PLAYLIST_ID,
                    position = 0,
                    trackId = OLD_TRACK_ID,
                    addedByUserId = OWNER_ID,
                    addedAtEpochMs = NOW.toEpochMilli(),
                ),
            ),
        )
        val remote = FailingProgressiveRemote(partialDetail())
        val repository = repository(remote, UnconfinedTestDispatcher(testScheduler))

        val result = repository.refreshPlaylist(PLAYLIST_ID)

        assertThat(result).isInstanceOf(PlaylistResult.Failure::class.java)
        assertThat(database.playlistDao().entries(OWNER_ID, PLAYLIST_ID).map { it.id })
            .containsExactly(OLD_ENTRY_ID)
    }

    @Test
    fun failedProgressiveRefreshDoesNotPersistPartialEntriesIntoEmptyPlaylist() = runTest {
        database.playlistDao().upsertPlaylist(
            playlistEntity(name = "Cached", version = 1, trackCount = 2),
        )
        val repository =
            repository(
                FailingProgressiveRemote(partialDetail()),
                UnconfinedTestDispatcher(testScheduler),
            )

        val result = repository.refreshPlaylist(PLAYLIST_ID)

        assertThat(result).isInstanceOf(PlaylistResult.Failure::class.java)
        assertThat(database.playlistDao().entries(OWNER_ID, PLAYLIST_ID)).isEmpty()
        assertThat(database.catalogDao().track(NEW_TRACK_ID)).isNull()
        val stored = database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)
        assertThat(stored?.name).isEqualTo("Cached")
        assertThat(stored?.version).isEqualTo(1)
    }

    @Test
    fun nonProgressiveRefreshPersistsCompleteDetailOnlyOnce() = runTest {
        database.playlistDao().upsertPlaylist(playlistEntity(name = "Cached", version = 1))
        val repository =
            repository(
                CompleteDetailRemote(partialDetail().copy(trackCount = 1, nextCursor = null)),
                UnconfinedTestDispatcher(testScheduler),
            )
        executedQueries.clear()

        val result = repository.refreshPlaylist(PLAYLIST_ID)

        assertThat(result).isInstanceOf(PlaylistResult.Success::class.java)
        assertThat(
            executedQueries.count { query ->
                query.contains("DELETE FROM track_artist_credits", ignoreCase = true)
            },
        ).isEqualTo(1)
    }

    @Test
    fun serverGenerationChangeRejectsOldDetailBeforeCatalogWrite() = runTest {
        database.playlistDao().upsertPlaylist(playlistEntity(name = "Cached", version = 1))
        val serverRuntimeCoordinator = ServerRuntimeCoordinator()
        val remote =
            BlockingCompleteDetailRemote(
                partialDetail().copy(trackCount = 1, nextCursor = null),
            )
        val repository =
            repository(
                remote,
                UnconfinedTestDispatcher(testScheduler),
                serverRuntimeCoordinator,
            )

        val refresh = async { repository.refreshPlaylist(PLAYLIST_ID) }
        remote.requestStarted.await()
        val switchGeneration = serverRuntimeCoordinator.beginSwitch()
        serverRuntimeCoordinator.finishSwitch(switchGeneration)
        remote.releaseRequest.complete(Unit)

        assertThat(refresh.await()).isInstanceOf(PlaylistResult.Failure::class.java)
        assertThat(database.catalogDao().track(NEW_TRACK_ID)).isNull()
        assertThat(database.playlistDao().entries(OWNER_ID, PLAYLIST_ID)).isEmpty()
    }

    @Test
    fun incompleteSnapshotIsNotObservableAndServerStillDecidesReorder() = runTest {
        database.catalogDao().upsertTrack(track(CANCEL_TRACK_ID))
        database.playlistDao().upsertPlaylist(
            playlistEntity(name = "Incomplete", version = 1, trackCount = 2),
        )
        val remote = CountingMutationRemote()
        val repository = repository(remote, UnconfinedTestDispatcher(testScheduler))

        assertThat(repository.observePlaylist(PLAYLIST_ID).first()).isNull()
        val reorderResult =
            repository.reorder(
                ReorderPlaylistCommand(PLAYLIST_ID, 1, emptyList()),
            )

        assertThat(reorderResult).isInstanceOf(PlaylistResult.Failure::class.java)
        assertThat(remote.addCalls).isEqualTo(0)
        assertThat(remote.reorderCalls).isEqualTo(1)
        assertThat(database.playlistDao().entries(OWNER_ID, PLAYLIST_ID)).isEmpty()
        assertThat(
            database
                .pendingSyncOperationDao()
                .forTarget(OWNER_ID, SyncTargetType.PLAYLIST, PLAYLIST_ID),
        ).isEmpty()
    }

    @Test
    fun incompleteSummaryCanAddTrackOnlineAndRemainsExplicitlyIncomplete() = runTest {
        database.catalogDao().upsertTrack(track(NEW_TRACK_ID))
        database.playlistDao().upsertPlaylist(
            playlistEntity(name = "Incomplete", version = 1, trackCount = 2),
        )
        val mutation =
            PlaylistEntryMutationDto(
                playlistId = PLAYLIST_ID,
                version = 2,
                updatedAt = NOW.plusSeconds(1).toString(),
                entry = playlistEntryDto(NEW_ENTRY_ID, 2, NEW_TRACK_ID),
            )
        val remote = SuccessfulAddRemote(mutation)
        val repository = repository(remote, UnconfinedTestDispatcher(testScheduler))

        val result =
            repository.addTrack(
                AddPlaylistTrackCommand(PLAYLIST_ID, 1, NEW_TRACK_ID),
            )

        assertThat(result).isInstanceOf(PlaylistResult.Success::class.java)
        assertThat(remote.requests).containsExactly(
            AddPlaylistTrackRequestDto(
                expectedVersion = 1,
                trackId = NEW_TRACK_ID,
                insertAfterEntryId = null,
            ),
        )
        val stored = requireNotNull(database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID))
        assertThat(stored.version).isEqualTo(2)
        assertThat(stored.trackCount).isEqualTo(3)
        assertThat(database.playlistDao().entries(OWNER_ID, PLAYLIST_ID)).isEmpty()
        assertThat(repository.observePlaylist(PLAYLIST_ID).first()).isNull()
        assertThat(pendingForPlaylist()).isEmpty()
    }

    @Test
    fun staleAddVersionRefreshesMetadataAndRetriesOnce() = runTest {
        database.playlistDao().upsertPlaylist(
            playlistEntity(name = "Stale", version = 1, trackCount = 2),
        )
        val latestPage =
            PlaylistDetailDto(
                id = PLAYLIST_ID,
                owner = ownerSummary(),
                name = "Fresh",
                description = null,
                visibility = "PRIVATE",
                cover = null,
                trackCount = 2,
                version = 2,
                createdAt = NOW.toString(),
                updatedAt = NOW.plusSeconds(1).toString(),
                entries = emptyList(),
                nextCursor = "more",
            )
        val mutation =
            PlaylistEntryMutationDto(
                playlistId = PLAYLIST_ID,
                version = 3,
                updatedAt = NOW.plusSeconds(2).toString(),
                entry = playlistEntryDto(NEW_ENTRY_ID, 2, NEW_TRACK_ID),
            )
        val remote = StaleThenSuccessfulAddRemote(latestPage, mutation)
        val repository = repository(remote, UnconfinedTestDispatcher(testScheduler))

        val result = repository.addTrack(AddPlaylistTrackCommand(PLAYLIST_ID, 1, NEW_TRACK_ID))

        assertThat(result).isInstanceOf(PlaylistResult.Success::class.java)
        assertThat(remote.requests.map(AddPlaylistTrackRequestDto::expectedVersion)).containsExactly(1L, 2L).inOrder()
        assertThat(remote.idempotencyKeys).hasSize(2)
        assertThat(remote.idempotencyKeys.distinct()).hasSize(2)
        assertThat(remote.metadataLimits).containsExactly(1)
        val stored = requireNotNull(database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID))
        assertThat(stored.version).isEqualTo(3)
        assertThat(stored.trackCount).isEqualTo(3)
        assertThat(database.playlistDao().entries(OWNER_ID, PLAYLIST_ID)).isEmpty()
    }

    @Test
    fun successfulServerMutationsRefreshCompleteCache() = runTest {
        database.catalogDao().upsertTrack(track(CANCEL_TRACK_ID))
        database.catalogDao().upsertTrack(track(SECOND_TRACK_ID))
        database.playlistDao().replacePlaylist(
            playlistEntity(name = "Original", version = 1, trackCount = 2),
            listOf(
                playlistEntry(FIRST_ENTRY_ID, 0, CANCEL_TRACK_ID),
                playlistEntry(SECOND_ENTRY_ID, 1, SECOND_TRACK_ID),
            ),
        )
        val remote =
            SuccessfulMutationRemote(
                updatedSummary = summary(name = "Updated", version = 2, trackCount = 2),
                addedMutation =
                PlaylistEntryMutationDto(
                    playlistId = PLAYLIST_ID,
                    version = 3,
                    updatedAt = NOW.plusSeconds(2).toString(),
                    entry = playlistEntryDto(NEW_ENTRY_ID, 2, NEW_TRACK_ID),
                ),
            )
        val repository = repository(remote, UnconfinedTestDispatcher(testScheduler))

        assertThat(
            repository.update(
                UpdatePlaylistCommand(PLAYLIST_ID, 1, name = ValueChange.Set("Updated")),
            ),
        ).isInstanceOf(PlaylistResult.Success::class.java)
        assertThat(database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)?.name).isEqualTo("Updated")
        assertThat(database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)?.version).isEqualTo(2)

        assertThat(
            repository.addTrack(
                AddPlaylistTrackCommand(PLAYLIST_ID, 2, NEW_TRACK_ID),
            ),
        ).isInstanceOf(PlaylistResult.Success::class.java)
        assertThat(database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)?.trackCount).isEqualTo(3)
        assertThat(database.playlistDao().entries(OWNER_ID, PLAYLIST_ID).map { it.id })
            .containsExactly(FIRST_ENTRY_ID, SECOND_ENTRY_ID, NEW_ENTRY_ID)
            .inOrder()

        assertThat(
            repository.removeTrack(
                RemovePlaylistTrackCommand(PLAYLIST_ID, FIRST_ENTRY_ID, 3),
            ),
        ).isInstanceOf(PlaylistResult.Success::class.java)
        assertThat(database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)?.version).isEqualTo(4)
        assertThat(database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)?.trackCount).isEqualTo(2)
        assertThat(database.playlistDao().entries(OWNER_ID, PLAYLIST_ID).map { it.id })
            .containsExactly(SECOND_ENTRY_ID, NEW_ENTRY_ID)
            .inOrder()

        assertThat(
            repository.reorder(
                ReorderPlaylistCommand(
                    PLAYLIST_ID,
                    4,
                    listOf(NEW_ENTRY_ID, SECOND_ENTRY_ID),
                ),
            ),
        ).isInstanceOf(PlaylistResult.Success::class.java)
        assertThat(database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)?.version).isEqualTo(5)
        assertThat(database.playlistDao().entries(OWNER_ID, PLAYLIST_ID).map { it.id })
            .containsExactly(NEW_ENTRY_ID, SECOND_ENTRY_ID)
            .inOrder()

        assertThat(repository.delete(PLAYLIST_ID, 5)).isInstanceOf(PlaylistResult.Success::class.java)
        assertThat(database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)).isNull()
        assertThat(database.playlistDao().entries(OWNER_ID, PLAYLIST_ID)).isEmpty()
        assertThat(pendingForPlaylist()).isEmpty()
    }

    @Test
    fun cancelledAddKeepsCachedEntriesAndDoesNotQueueRetry() = runTest {
        database.catalogDao().upsertTrack(track(CANCEL_TRACK_ID))
        database.playlistDao().upsertPlaylist(playlistEntity(name = "Cached", version = 1))
        val remote = BlockingMutationRemote()
        val repository = repository(remote, UnconfinedTestDispatcher(testScheduler))

        val mutation =
            async {
                repository.addTrack(
                    AddPlaylistTrackCommand(PLAYLIST_ID, 1, CANCEL_TRACK_ID),
                )
            }
        remote.addStarted.await()
        mutation.cancelAndJoin()

        assertThat(mutation.isCancelled).isTrue()
        assertThat(database.playlistDao().entries(OWNER_ID, PLAYLIST_ID)).isEmpty()
        assertThat(database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)?.version).isEqualTo(1)
        assertThat(pendingForPlaylist()).isEmpty()
    }

    @Test
    fun cancelledDeleteKeepsCachedPlaylistAndDoesNotQueueRetry() = runTest {
        database.catalogDao().upsertTrack(track(CANCEL_TRACK_ID))
        database.playlistDao().replacePlaylist(
            playlistEntity(name = "Cached", version = 1, trackCount = 1),
            listOf(playlistEntry(FIRST_ENTRY_ID, 0, CANCEL_TRACK_ID)),
        )
        val remote = BlockingMutationRemote()
        val repository = repository(remote, UnconfinedTestDispatcher(testScheduler))

        val mutation = async { repository.delete(PLAYLIST_ID, 1) }
        remote.deleteStarted.await()
        mutation.cancelAndJoin()

        assertThat(mutation.isCancelled).isTrue()
        assertThat(database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)?.version).isEqualTo(1)
        assertThat(database.playlistDao().entries(OWNER_ID, PLAYLIST_ID).map { it.id })
            .containsExactly(FIRST_ENTRY_ID)
        assertThat(pendingForPlaylist()).isEmpty()
    }

    @Test
    fun cancelledRemoveKeepsCachedEntryAndDoesNotQueueRetry() = runTest {
        database.catalogDao().upsertTrack(track(CANCEL_TRACK_ID))
        database.playlistDao().replacePlaylist(
            playlistEntity(name = "Cached", version = 1, trackCount = 1),
            listOf(playlistEntry(FIRST_ENTRY_ID, 0, CANCEL_TRACK_ID)),
        )
        val remote = BlockingMutationRemote()
        val repository = repository(remote, UnconfinedTestDispatcher(testScheduler))

        val mutation =
            async {
                repository.removeTrack(
                    RemovePlaylistTrackCommand(PLAYLIST_ID, FIRST_ENTRY_ID, 1),
                )
            }
        remote.removeStarted.await()
        mutation.cancelAndJoin()

        assertThat(mutation.isCancelled).isTrue()
        assertThat(database.playlistDao().entries(OWNER_ID, PLAYLIST_ID).map { it.id })
            .containsExactly(FIRST_ENTRY_ID)
        assertThat(database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)?.version).isEqualTo(1)
        assertThat(pendingForPlaylist()).isEmpty()
    }

    @Test
    fun cancelledReorderKeepsCachedOrderAndDoesNotQueueRetry() = runTest {
        database.catalogDao().upsertTrack(track(CANCEL_TRACK_ID))
        database.catalogDao().upsertTrack(track(SECOND_TRACK_ID))
        database.playlistDao().replacePlaylist(
            playlistEntity(name = "Cached", version = 1, trackCount = 2),
            listOf(
                playlistEntry(FIRST_ENTRY_ID, 0, CANCEL_TRACK_ID),
                playlistEntry(SECOND_ENTRY_ID, 1, SECOND_TRACK_ID),
            ),
        )
        val remote = BlockingMutationRemote()
        val repository = repository(remote, UnconfinedTestDispatcher(testScheduler))

        val mutation =
            async {
                repository.reorder(
                    ReorderPlaylistCommand(PLAYLIST_ID, 1, listOf(SECOND_ENTRY_ID, FIRST_ENTRY_ID)),
                )
            }
        remote.reorderStarted.await()
        mutation.cancelAndJoin()

        assertThat(mutation.isCancelled).isTrue()
        assertThat(database.playlistDao().entries(OWNER_ID, PLAYLIST_ID).map { it.id })
            .containsExactly(FIRST_ENTRY_ID, SECOND_ENTRY_ID)
            .inOrder()
        assertThat(database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)?.version).isEqualTo(1)
        assertThat(pendingForPlaylist()).isEmpty()
    }

    @Test
    fun networkFailuresDoNotMutateCacheOrQueueRetries() = runTest {
        database.catalogDao().upsertTrack(track(CANCEL_TRACK_ID))
        database.catalogDao().upsertTrack(track(SECOND_TRACK_ID))
        database.catalogDao().upsertTrack(track(NEW_TRACK_ID))
        database.playlistDao().replacePlaylist(
            playlistEntity(name = "Original", version = 1, trackCount = 2),
            listOf(
                playlistEntry(FIRST_ENTRY_ID, 0, CANCEL_TRACK_ID),
                playlistEntry(SECOND_ENTRY_ID, 1, SECOND_TRACK_ID),
            ),
        )
        val repository =
            repository(NetworkFailingMutationRemote, UnconfinedTestDispatcher(testScheduler))
        val mutations =
            listOf<suspend () -> PlaylistResult<*>>(
                {
                    repository.update(
                        UpdatePlaylistCommand(PLAYLIST_ID, 1, name = ValueChange.Set("Updated")),
                    )
                },
                { repository.delete(PLAYLIST_ID, 1) },
                {
                    repository.addTrack(
                        AddPlaylistTrackCommand(PLAYLIST_ID, 1, NEW_TRACK_ID),
                    )
                },
                {
                    repository.removeTrack(
                        RemovePlaylistTrackCommand(PLAYLIST_ID, FIRST_ENTRY_ID, 1),
                    )
                },
                {
                    repository.reorder(
                        ReorderPlaylistCommand(
                            PLAYLIST_ID,
                            1,
                            listOf(SECOND_ENTRY_ID, FIRST_ENTRY_ID),
                        ),
                    )
                },
            )

        mutations.forEach { mutation ->
            assertThat(mutation()).isInstanceOf(PlaylistResult.Failure::class.java)
            val stored = database.playlistDao().playlist(OWNER_ID, PLAYLIST_ID)
            assertThat(stored?.name).isEqualTo("Original")
            assertThat(stored?.version).isEqualTo(1)
            assertThat(stored?.trackCount).isEqualTo(2)
            assertThat(database.playlistDao().entries(OWNER_ID, PLAYLIST_ID).map { it.id })
                .containsExactly(FIRST_ENTRY_ID, SECOND_ENTRY_ID)
                .inOrder()
            assertThat(pendingForPlaylist()).isEmpty()
        }
    }

    private suspend fun pendingForPlaylist() = database
        .pendingSyncOperationDao()
        .forTarget(OWNER_ID, SyncTargetType.PLAYLIST, PLAYLIST_ID)

    private fun repository(
        remote: PlaylistRemoteDataSource,
        dispatcher: CoroutineDispatcher,
        serverRuntimeCoordinator: ServerRuntimeCoordinator = ServerRuntimeCoordinator(),
    ) = DefaultPlaylistRepository(
        database = database,
        playlistDao = database.playlistDao(),
        catalogDao = database.catalogDao(),
        catalogLocal = RoomCatalogLocalDataSource(database.catalogDao()),
        remote = remote,
        sessionProvider = SignedInSessionProvider(OWNER_ID),
        sessionMutationCoordinator = SessionMutationCoordinator(),
        serverRuntimeCoordinator = serverRuntimeCoordinator,
        clock = CLOCK,
        ioDispatcher = dispatcher,
    )

    private fun playlistEntity(name: String, version: Long, trackCount: Int = 0) = PlaylistEntity(
        ownerUserId = OWNER_ID,
        id = PLAYLIST_ID,
        name = name,
        description = null,
        visibility = StoredPlaylistVisibility.PRIVATE,
        cover = null,
        trackCount = trackCount,
        version = version,
        createdAtEpochMs = NOW.toEpochMilli(),
        updatedAtEpochMs = NOW.toEpochMilli(),
    )

    private fun track(id: String) = TrackEntity(
        id = id,
        albumId = null,
        title = id,
        durationMs = 180_000,
        trackNumber = null,
        discNumber = 1,
        publishedAtEpochMs = NOW.toEpochMilli(),
        artwork = null,
        cachedAtEpochMs = NOW.toEpochMilli(),
    )

    private fun playlistEntry(id: String, position: Int, trackId: String) = PlaylistEntryEntity(
        ownerUserId = OWNER_ID,
        id = id,
        playlistId = PLAYLIST_ID,
        position = position,
        trackId = trackId,
        addedByUserId = OWNER_ID,
        addedAtEpochMs = NOW.toEpochMilli(),
    )

    private fun summary(name: String, version: Long, trackCount: Int = 0) = PlaylistSummaryDto(
        id = PLAYLIST_ID,
        owner = ownerSummary(),
        name = name,
        description = null,
        visibility = "PRIVATE",
        cover = null,
        trackCount = trackCount,
        version = version,
        createdAt = NOW.toString(),
        updatedAt = NOW.toString(),
    )

    private fun partialDetail() = PlaylistDetailDto(
        id = PLAYLIST_ID,
        owner = ownerSummary(),
        name = "Remote",
        description = null,
        visibility = "PRIVATE",
        cover = null,
        trackCount = 2,
        version = 2,
        createdAt = NOW.toString(),
        updatedAt = NOW.toString(),
        entries =
        listOf(
            PlaylistEntryDto(
                id = NEW_ENTRY_ID,
                position = 0,
                track =
                TrackSummaryDto(
                    id = NEW_TRACK_ID,
                    title = "New track",
                    artists = listOf(ArtistReferenceDto(ARTIST_ID, "Artist")),
                    album = null,
                    artwork = null,
                    durationMs = 180_000,
                    trackNumber = null,
                    discNumber = 1,
                    isFavorite = false,
                    publishedAt = NOW.toString(),
                ),
                addedBy = ownerSummary(),
                addedAt = NOW.toString(),
            ),
        ),
        nextCursor = "next-page",
    )

    private fun ownerSummary() = UserSummaryDto(OWNER_ID, "owner", "Owner", null)

    private fun playlistEntryDto(id: String, position: Int, trackId: String) = PlaylistEntryDto(
        id = id,
        position = position,
        track =
        TrackSummaryDto(
            id = trackId,
            title = trackId,
            artists = listOf(ArtistReferenceDto(ARTIST_ID, "Artist")),
            album = null,
            artwork = null,
            durationMs = 180_000,
            trackNumber = null,
            discNumber = 1,
            isFavorite = false,
            publishedAt = NOW.toString(),
        ),
        addedBy = ownerSummary(),
        addedAt = NOW.toString(),
    )

    private class SignedInSessionProvider(ownerUserId: String) : AppSessionProvider {
        override val sessionState =
            MutableStateFlow<AppSessionState>(
                AppSessionState.SignedIn(ownerUserId),
            )

        override suspend fun restoreSession() = Unit
    }

    private abstract class PlaylistRemoteStub : PlaylistRemoteDataSource {
        open override suspend fun allPlaylists(sort: String): List<PlaylistSummaryDto> = error("unused")

        open override suspend fun playlist(playlistId: String): PlaylistDetailDto = error("unused")

        open override suspend fun create(
            idempotencyKey: String,
            request: CreatePlaylistRequestDto,
        ): PlaylistSummaryDto = error("unused")

        open override suspend fun update(
            playlistId: String,
            idempotencyKey: String,
            payload: PlaylistUpdatePayload,
        ): PlaylistSummaryDto = error("unused")

        open override suspend fun delete(playlistId: String, expectedVersion: Long, idempotencyKey: String): Unit =
            error("unused")

        open override suspend fun addTrack(
            playlistId: String,
            idempotencyKey: String,
            request: AddPlaylistTrackRequestDto,
        ): PlaylistEntryMutationDto = error("unused")

        open override suspend fun removeTrack(
            playlistId: String,
            entryId: String,
            expectedVersion: Long,
            idempotencyKey: String,
        ): PlaylistMutationDto = error("unused")

        open override suspend fun reorder(
            playlistId: String,
            idempotencyKey: String,
            request: ReorderPlaylistRequestDto,
        ): PlaylistMutationDto = error("unused")
    }

    private class BlockingUpdateRemote : PlaylistRemoteStub() {
        val updateStarted = CompletableDeferred<Unit>()
        val releaseUpdate = CompletableDeferred<Unit>()
        var updateCalls: Int = 0
        private var currentVersion: Long = 1

        override suspend fun update(
            playlistId: String,
            idempotencyKey: String,
            payload: PlaylistUpdatePayload,
        ): PlaylistSummaryDto {
            updateCalls += 1
            updateStarted.complete(Unit)
            if (payload.expectedVersion != currentVersion) {
                throw versionConflict(payload.expectedVersion, currentVersion)
            }
            releaseUpdate.await()
            currentVersion += 1
            return summary(requireNotNull(payload.name), currentVersion)
        }

        private fun summary(name: String, version: Long) = PlaylistSummaryDto(
            id = PLAYLIST_ID,
            owner = UserSummaryDto(OWNER_ID, "owner", "Owner", null),
            name = name,
            description = null,
            visibility = "PRIVATE",
            cover = null,
            trackCount = 0,
            version = version,
            createdAt = NOW.toString(),
            updatedAt = NOW.toString(),
        )
    }

    private class BlockingListRemote(private val items: List<PlaylistSummaryDto>) : PlaylistRemoteStub() {
        val requestStarted = CompletableDeferred<Unit>()
        val releaseRequest = CompletableDeferred<Unit>()

        override suspend fun allPlaylists(sort: String): List<PlaylistSummaryDto> {
            requestStarted.complete(Unit)
            releaseRequest.await()
            return items
        }
    }

    private class FailingProgressiveRemote(private val firstPage: PlaylistDetailDto) : PlaylistRemoteStub() {
        override suspend fun playlistProgressively(
            playlistId: String,
            onFirstPage: suspend (PlaylistDetailDto) -> Unit,
        ): PlaylistDetailDto {
            onFirstPage(firstPage)
            throw IOException("Second page failed")
        }
    }

    private class CompleteDetailRemote(private val detail: PlaylistDetailDto) : PlaylistRemoteStub() {
        override suspend fun playlist(playlistId: String): PlaylistDetailDto = detail
    }

    private class BlockingCompleteDetailRemote(private val detail: PlaylistDetailDto) : PlaylistRemoteStub() {
        val requestStarted = CompletableDeferred<Unit>()
        val releaseRequest = CompletableDeferred<Unit>()

        override suspend fun playlist(playlistId: String): PlaylistDetailDto {
            requestStarted.complete(Unit)
            releaseRequest.await()
            return detail
        }
    }

    private class CountingMutationRemote : PlaylistRemoteStub() {
        var addCalls = 0
        var reorderCalls = 0

        override suspend fun addTrack(
            playlistId: String,
            idempotencyKey: String,
            request: AddPlaylistTrackRequestDto,
        ): PlaylistEntryMutationDto {
            addCalls += 1
            error("Unexpected add request")
        }

        override suspend fun reorder(
            playlistId: String,
            idempotencyKey: String,
            request: ReorderPlaylistRequestDto,
        ): PlaylistMutationDto {
            reorderCalls += 1
            error("Unexpected reorder request")
        }
    }

    private class BlockingMutationRemote : PlaylistRemoteStub() {
        val addStarted = CompletableDeferred<Unit>()
        val deleteStarted = CompletableDeferred<Unit>()
        val removeStarted = CompletableDeferred<Unit>()
        val reorderStarted = CompletableDeferred<Unit>()

        override suspend fun delete(playlistId: String, expectedVersion: Long, idempotencyKey: String) {
            deleteStarted.complete(Unit)
            awaitCancellation()
        }

        override suspend fun addTrack(
            playlistId: String,
            idempotencyKey: String,
            request: AddPlaylistTrackRequestDto,
        ): PlaylistEntryMutationDto {
            addStarted.complete(Unit)
            awaitCancellation()
        }

        override suspend fun removeTrack(
            playlistId: String,
            entryId: String,
            expectedVersion: Long,
            idempotencyKey: String,
        ): PlaylistMutationDto {
            removeStarted.complete(Unit)
            awaitCancellation()
        }

        override suspend fun reorder(
            playlistId: String,
            idempotencyKey: String,
            request: ReorderPlaylistRequestDto,
        ): PlaylistMutationDto {
            reorderStarted.complete(Unit)
            awaitCancellation()
        }
    }

    private class SuccessfulAddRemote(private val mutation: PlaylistEntryMutationDto) : PlaylistRemoteStub() {
        val requests = mutableListOf<AddPlaylistTrackRequestDto>()

        override suspend fun addTrack(
            playlistId: String,
            idempotencyKey: String,
            request: AddPlaylistTrackRequestDto,
        ): PlaylistEntryMutationDto {
            requests += request
            return mutation
        }
    }

    private class StaleThenSuccessfulAddRemote(
        private val latestPage: PlaylistDetailDto,
        private val mutation: PlaylistEntryMutationDto,
    ) : PlaylistRemoteStub() {
        val requests = mutableListOf<AddPlaylistTrackRequestDto>()
        val idempotencyKeys = mutableListOf<String>()
        val metadataLimits = mutableListOf<Int>()

        override suspend fun playlistPage(playlistId: String, cursor: String?, limit: Int): PlaylistDetailDto {
            metadataLimits += limit
            return latestPage
        }

        override suspend fun addTrack(
            playlistId: String,
            idempotencyKey: String,
            request: AddPlaylistTrackRequestDto,
        ): PlaylistEntryMutationDto {
            requests += request
            idempotencyKeys += idempotencyKey
            if (requests.size == 1) throw versionConflict(request.expectedVersion, latestPage.version)
            return mutation
        }
    }

    private class SuccessfulMutationRemote(
        private val updatedSummary: PlaylistSummaryDto,
        private val addedMutation: PlaylistEntryMutationDto,
    ) : PlaylistRemoteStub() {
        override suspend fun update(
            playlistId: String,
            idempotencyKey: String,
            payload: PlaylistUpdatePayload,
        ): PlaylistSummaryDto = updatedSummary

        override suspend fun delete(playlistId: String, expectedVersion: Long, idempotencyKey: String) = Unit

        override suspend fun addTrack(
            playlistId: String,
            idempotencyKey: String,
            request: AddPlaylistTrackRequestDto,
        ): PlaylistEntryMutationDto = addedMutation

        override suspend fun removeTrack(
            playlistId: String,
            entryId: String,
            expectedVersion: Long,
            idempotencyKey: String,
        ) = PlaylistMutationDto(playlistId, expectedVersion + 1, NOW.plusSeconds(3).toString())

        override suspend fun reorder(playlistId: String, idempotencyKey: String, request: ReorderPlaylistRequestDto) =
            PlaylistMutationDto(playlistId, request.expectedVersion + 1, NOW.plusSeconds(4).toString())
    }

    private data object NetworkFailingMutationRemote : PlaylistRemoteStub() {
        override suspend fun update(
            playlistId: String,
            idempotencyKey: String,
            payload: PlaylistUpdatePayload,
        ): PlaylistSummaryDto = throw IOException("offline")

        override suspend fun delete(playlistId: String, expectedVersion: Long, idempotencyKey: String): Unit =
            throw IOException("offline")

        override suspend fun addTrack(
            playlistId: String,
            idempotencyKey: String,
            request: AddPlaylistTrackRequestDto,
        ): PlaylistEntryMutationDto = throw IOException("offline")

        override suspend fun removeTrack(
            playlistId: String,
            entryId: String,
            expectedVersion: Long,
            idempotencyKey: String,
        ): PlaylistMutationDto = throw IOException("offline")

        override suspend fun reorder(
            playlistId: String,
            idempotencyKey: String,
            request: ReorderPlaylistRequestDto,
        ): PlaylistMutationDto = throw IOException("offline")
    }

    private companion object {
        val OWNER_ID: String = UUID.randomUUID().toString()
        val PLAYLIST_ID: String = UUID.randomUUID().toString()
        const val OLD_ENTRY_ID = "entry-old"
        const val OLD_TRACK_ID = "00000000-0000-0000-0000-000000000101"
        const val NEW_ENTRY_ID = "00000000-0000-0000-0000-000000000104"
        const val NEW_TRACK_ID = "00000000-0000-0000-0000-000000000102"
        const val ARTIST_ID = "00000000-0000-0000-0000-000000000103"
        val FIRST_ENTRY_ID: String = UUID.randomUUID().toString()
        val SECOND_ENTRY_ID: String = UUID.randomUUID().toString()
        val CANCEL_TRACK_ID: String = UUID.randomUUID().toString()
        val SECOND_TRACK_ID: String = UUID.randomUUID().toString()
        val NOW: Instant = Instant.parse("2026-07-12T00:00:00Z")
        val CLOCK: Clock = Clock.fixed(NOW, ZoneOffset.UTC)

        fun versionConflict(expectedVersion: Long, currentVersion: Long) = PlaylistRemoteException(
            error = DomainError.Conflict("stale", null, ProblemCode.VersionConflict),
            conflict =
            PlaylistVersionConflict(
                playlistId = PLAYLIST_ID,
                expectedVersion = expectedVersion,
                currentVersion = currentVersion,
                conflictFields = emptySet(),
            ),
        )
    }
}
