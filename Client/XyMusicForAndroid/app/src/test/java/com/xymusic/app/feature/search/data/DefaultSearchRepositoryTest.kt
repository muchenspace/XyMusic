package com.xymusic.app.feature.search.data

import android.app.Application
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import app.cash.turbine.test
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.data.media.RoomCatalogLocalDataSource
import com.xymusic.app.core.data.media.remote.AlbumSummaryDto
import com.xymusic.app.core.data.media.remote.ArtistSummaryDto
import com.xymusic.app.core.data.media.remote.RemotePage
import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.feature.search.data.remote.SearchOverviewRemote
import com.xymusic.app.feature.search.data.remote.SearchRemoteDataSource
import com.xymusic.app.feature.search.domain.SearchResult
import com.xymusic.app.feature.search.domain.model.SearchOverview
import com.xymusic.app.feature.search.domain.model.SearchQuery
import com.xymusic.app.feature.search.domain.model.SearchScope
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import java.util.UUID
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class DefaultSearchRepositoryTest {
    private lateinit var database: XyMusicDatabase
    private lateinit var sessionProvider: MutableSessionProvider
    private lateinit var mutationCoordinator: SessionMutationCoordinator

    @Before
    fun setUp() {
        database =
            Room
                .inMemoryDatabaseBuilder(
                    ApplicationProvider.getApplicationContext(),
                    XyMusicDatabase::class.java,
                ).allowMainThreadQueries()
                .build()
        sessionProvider = MutableSessionProvider()
        mutationCoordinator = SessionMutationCoordinator()
    }

    @After
    fun tearDown() {
        database.close()
    }

    @Test
    fun historyObservationSwitchesOwnersWithTheSession() = runTest {
        val repository = repository()
        val query = SearchQuery.from("hello")

        repository.observeHistory().test {
            assertThat(awaitItem()).isEmpty()

            sessionProvider.signIn(OWNER_A)
            assertThat(awaitItem()).isEmpty()
            assertThat(repository.record(query, SearchScope.TRACKS))
                .isEqualTo(SearchResult.Success(Unit))
            assertThat(awaitItem().map { it.query.value }).containsExactly("hello")

            sessionProvider.signIn(OWNER_B)
            assertThat(awaitItem()).isEmpty()
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun queuedHistoryMutationCannotWriteAfterOwnerChanges() = runTest {
        val repository = repository()
        val lockEntered = CompletableDeferred<Unit>()
        val releaseLock = CompletableDeferred<Unit>()
        sessionProvider.signIn(OWNER_A)
        val blocker =
            async(start = CoroutineStart.UNDISPATCHED) {
                mutationCoordinator.mutate {
                    lockEntered.complete(Unit)
                    releaseLock.await()
                }
            }
        lockEntered.await()
        val mutation =
            async(start = CoroutineStart.UNDISPATCHED) {
                repository.record(SearchQuery.from("blocked"), SearchScope.ALL)
            }

        sessionProvider.signIn(OWNER_B)
        releaseLock.complete(Unit)
        blocker.await()

        assertThat(mutation.await()).isInstanceOf(SearchResult.Failure::class.java)
        assertThat(database.searchHistoryDao().observe(OWNER_A).first()).isEmpty()
    }

    private fun repository(): DefaultSearchRepository {
        val runtime = ServerRuntimeCoordinator()
        val remote = UnusedSearchRemoteDataSource
        val store = EmptySearchOverviewStore
        return DefaultSearchRepository(
            database = database,
            remoteKeyDao = database.catalogRemoteKeyDao(),
            catalogLocal = RoomCatalogLocalDataSource(database.catalogDao()),
            remote = remote,
            overviewStore = store,
            overviewRefresher = SearchOverviewRefresher(remote, store, runtime),
            searchHistoryDao = database.searchHistoryDao(),
            sessionProvider = sessionProvider,
            sessionMutationCoordinator = mutationCoordinator,
            clock = Clock.fixed(Instant.parse("2026-07-13T00:00:00Z"), ZoneOffset.UTC),
            serverRuntimeCoordinator = runtime,
        )
    }

    private class MutableSessionProvider : AppSessionProvider {
        override val sessionState = MutableStateFlow<AppSessionState>(AppSessionState.SignedOut)

        override suspend fun restoreSession() = Unit

        fun signIn(ownerUserId: String) {
            sessionState.value = AppSessionState.SignedIn(ownerUserId)
        }
    }

    private data object EmptySearchOverviewStore : SearchOverviewStore {
        override fun observe(query: SearchQuery): Flow<SearchOverview?> = flowOf(null)

        override suspend fun replace(query: SearchQuery, overview: SearchOverviewRemote) = Unit
    }

    private data object UnusedSearchRemoteDataSource : SearchRemoteDataSource {
        override suspend fun overview(query: SearchQuery): SearchOverviewRemote = error("unused")

        override suspend fun tracks(cursor: String?, query: SearchQuery): RemotePage<TrackSummaryDto> = error("unused")

        override suspend fun artists(cursor: String?, query: SearchQuery): RemotePage<ArtistSummaryDto> =
            error("unused")

        override suspend fun albums(cursor: String?, query: SearchQuery): RemotePage<AlbumSummaryDto> = error("unused")
    }

    private companion object {
        val OWNER_A: String = UUID.randomUUID().toString()
        val OWNER_B: String = UUID.randomUUID().toString()
    }
}
