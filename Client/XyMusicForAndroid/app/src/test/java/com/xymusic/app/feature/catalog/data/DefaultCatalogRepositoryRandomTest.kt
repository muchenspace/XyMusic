package com.xymusic.app.feature.catalog.data

import android.app.Application
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.data.media.RoomCatalogLocalDataSource
import com.xymusic.app.core.data.media.remote.AlbumDetailDto
import com.xymusic.app.core.data.media.remote.AlbumReferenceDto
import com.xymusic.app.core.data.media.remote.AlbumSummaryDto
import com.xymusic.app.core.data.media.remote.ArtistDetailDto
import com.xymusic.app.core.data.media.remote.ArtistReferenceDto
import com.xymusic.app.core.data.media.remote.ArtistSummaryDto
import com.xymusic.app.core.data.media.remote.RemotePage
import com.xymusic.app.core.data.media.remote.TrackDetailDto
import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.domain.error.DomainError
import com.xymusic.app.feature.catalog.data.remote.CatalogRemoteDataSource
import com.xymusic.app.feature.catalog.domain.CatalogResult
import com.xymusic.app.feature.catalog.domain.model.AlbumQuery
import com.xymusic.app.feature.catalog.domain.model.ArtistQuery
import com.xymusic.app.feature.catalog.domain.model.TrackQuery
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
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
class DefaultCatalogRepositoryRandomTest {
    private lateinit var database: XyMusicDatabase
    private lateinit var remote: FakeCatalogRemoteDataSource
    private val clock = Clock.fixed(Instant.parse("2026-07-13T00:00:00Z"), ZoneOffset.UTC)

    @Before
    fun setUp() {
        database =
            Room
                .inMemoryDatabaseBuilder(
                    ApplicationProvider.getApplicationContext(),
                    XyMusicDatabase::class.java,
                ).allowMainThreadQueries()
                .build()
        remote = FakeCatalogRemoteDataSource()
    }

    @After
    fun tearDown() {
        database.close()
    }

    @Test
    fun randomResultsAreReturnedInServerOrderAndMergedIntoCatalogCache() = runTest {
        remote.randomAlbumsResponse = listOf(album(ALBUM_ID_2), album(ALBUM_ID_1))
        remote.randomTracksResponse = listOf(track(TRACK_ID_2), track(TRACK_ID_1))

        val albums = repository().randomAlbums(limit = 2)
        val tracks = repository().randomTracks(limit = 16)

        val returnedAlbums =
            when (albums) {
                is CatalogResult.Success -> albums.value
                is CatalogResult.Failure -> error("Random albums failed: ${albums.error}")
            }
        val returnedTracks =
            when (tracks) {
                is CatalogResult.Success -> tracks.value
                is CatalogResult.Failure -> error("Random tracks failed: ${tracks.error}")
            }

        assertThat(returnedAlbums.map { it.id })
            .containsExactly(ALBUM_ID_2, ALBUM_ID_1)
            .inOrder()
        assertThat(returnedTracks.map { it.id })
            .containsExactly(TRACK_ID_2, TRACK_ID_1)
            .inOrder()
        assertThat(
            database
                .catalogDao()
                .observeAlbum(ALBUM_ID_2)
                .first()
                ?.album
                ?.id,
        ).isEqualTo(ALBUM_ID_2)
        assertThat(
            database
                .catalogDao()
                .observeTrack(TRACK_ID_2)
                .first()
                ?.track
                ?.id,
        ).isEqualTo(TRACK_ID_2)
    }

    @Test
    fun duplicateRandomIdsAreProtocolFailureAndDoNotMutateCache() = runTest {
        remote.randomTracksResponse = listOf(track(TRACK_ID_1), track(TRACK_ID_1))

        val result = repository().randomTracks(limit = 16)

        assertThat(result).isInstanceOf(CatalogResult.Failure::class.java)
        assertThat((result as CatalogResult.Failure).error)
            .isInstanceOf(DomainError.Protocol::class.java)
        assertThat(database.catalogDao().observeTrack(TRACK_ID_1).first()).isNull()
    }

    @Test
    fun oversizedRandomResponseIsProtocolFailure() = runTest {
        remote.randomAlbumsResponse =
            listOf(
                album(ALBUM_ID_1),
                album(ALBUM_ID_2),
                album(ALBUM_ID_3),
            )

        val result = repository().randomAlbums(limit = 2)

        assertThat(result).isInstanceOf(CatalogResult.Failure::class.java)
        assertThat((result as CatalogResult.Failure).error)
            .isInstanceOf(DomainError.Protocol::class.java)
    }

    @Test
    fun invalidLimitFailsBeforeCallingRemote() = runTest {
        val failure = runCatching { repository().randomAlbums(limit = 0) }.exceptionOrNull()

        assertThat(failure).isInstanceOf(IllegalArgumentException::class.java)
        assertThat(remote.randomAlbumCalls).isEqualTo(0)
    }

    @Test
    fun summaryAndReferenceMergesDoNotShortCircuitDetailRefreshAndForceRefreshBypassesTtl() = runTest {
        val local = RoomCatalogLocalDataSource(database.catalogDao())
        local.mergeArtistSummaries(
            listOf(ArtistSummaryDto(id = ARTIST_ID, name = "Summary Artist", artwork = null)),
            cachedAtEpochMs = clock.millis(),
        )
        local.mergeAlbumSummaries(
            listOf(album(ALBUM_ID_1)),
            cachedAtEpochMs = clock.millis(),
        )
        local.mergeTrackSummaries(
            listOf(track(TRACK_ID_1)),
            cachedAtEpochMs = clock.millis(),
        )

        remote.artistDetailResponse =
            ArtistDetailDto(
                id = ARTIST_ID,
                name = "Full Artist",
                artwork = null,
                description = "Full artist bio",
            )
        remote.albumDetailResponse =
            AlbumDetailDto(
                id = ALBUM_ID_1,
                title = "Full Album",
                artists = listOf(ArtistReferenceDto(ARTIST_ID, "Full Artist")),
                cover = null,
                releaseDate = "2026-07-13",
                trackCount = 8,
                description = "Full album liner notes",
            )

        val repo = repository()
        assertThat(repo.refreshArtist(ARTIST_ID)).isEqualTo(CatalogResult.Success(Unit))
        assertThat(repo.refreshAlbum(ALBUM_ID_1)).isEqualTo(CatalogResult.Success(Unit))
        assertThat(remote.artistDetailCalls).isEqualTo(1)
        assertThat(remote.albumDetailCalls).isEqualTo(1)
        assertThat(repo.observeArtist(ARTIST_ID).first()?.description).isEqualTo("Full artist bio")
        assertThat(repo.observeAlbum(ALBUM_ID_1).first()?.description).isEqualTo("Full album liner notes")

        // Within TTL, default refresh hits the detail cache even after subsequent summary merges.
        local.mergeArtistSummaries(
            listOf(ArtistSummaryDto(id = ARTIST_ID, name = "Full Artist", artwork = null)),
            cachedAtEpochMs = clock.millis() + 1_000,
        )
        local.mergeAlbumSummaries(
            listOf(album(ALBUM_ID_1)),
            cachedAtEpochMs = clock.millis() + 1_000,
        )
        assertThat(repo.refreshArtist(ARTIST_ID)).isEqualTo(CatalogResult.Success(Unit))
        assertThat(repo.refreshAlbum(ALBUM_ID_1)).isEqualTo(CatalogResult.Success(Unit))
        assertThat(remote.artistDetailCalls).isEqualTo(1)
        assertThat(remote.albumDetailCalls).isEqualTo(1)

        // Explicit forceRefresh bypasses TTL and fetches fresh detail payloads.
        remote.artistDetailResponse = remote.artistDetailResponse!!.copy(description = "Updated artist bio")
        remote.albumDetailResponse = remote.albumDetailResponse!!.copy(description = "Updated album notes")
        assertThat(repo.refreshArtist(ARTIST_ID, forceRefresh = true)).isEqualTo(CatalogResult.Success(Unit))
        assertThat(repo.refreshAlbum(ALBUM_ID_1, forceRefresh = true)).isEqualTo(CatalogResult.Success(Unit))
        assertThat(remote.artistDetailCalls).isEqualTo(2)
        assertThat(remote.albumDetailCalls).isEqualTo(2)
        assertThat(repo.observeArtist(ARTIST_ID).first()?.description).isEqualTo("Updated artist bio")
        assertThat(repo.observeAlbum(ALBUM_ID_1).first()?.description).isEqualTo("Updated album notes")
    }

    private fun repository(): DefaultCatalogRepository {
        val runtime = ServerRuntimeCoordinator()
        return DefaultCatalogRepository(
            database = database,
            remoteKeyDao = database.catalogRemoteKeyDao(),
            local = RoomCatalogLocalDataSource(database.catalogDao()),
            remote = remote,
            clock = clock,
            refreshExecutor =
            CatalogRefreshExecutor(
                transactionRunner = RoomCatalogTransactionRunner(database),
                clock = clock,
                serverRuntimeCoordinator = runtime,
            ),
            serverRuntimeCoordinator = runtime,
        )
    }

    private fun album(id: String) = AlbumSummaryDto(
        id = id,
        title = "Album $id",
        artists = listOf(ArtistReferenceDto(ARTIST_ID, "Artist")),
        cover = null,
        releaseDate = "2026-07-13",
        trackCount = 8,
    )

    private fun track(id: String) = TrackSummaryDto(
        id = id,
        title = "Track $id",
        artists = listOf(ArtistReferenceDto(ARTIST_ID, "Artist")),
        album = AlbumReferenceDto(ALBUM_ID_1, "Album"),
        artwork = null,
        durationMs = 180_000,
        trackNumber = 1,
        discNumber = 1,
        isFavorite = false,
        publishedAt = "2026-07-13T00:00:00Z",
    )

    private class FakeCatalogRemoteDataSource : CatalogRemoteDataSource {
        var randomAlbumsResponse: List<AlbumSummaryDto> = emptyList()
        var randomTracksResponse: List<TrackSummaryDto> = emptyList()
        var artistDetailResponse: ArtistDetailDto? = null
        var albumDetailResponse: AlbumDetailDto? = null
        var randomAlbumCalls = 0
        var artistDetailCalls = 0
        var albumDetailCalls = 0

        override suspend fun randomAlbums(limit: Int): List<AlbumSummaryDto> {
            randomAlbumCalls += 1
            return randomAlbumsResponse
        }

        override suspend fun randomTracks(limit: Int): List<TrackSummaryDto> = randomTracksResponse

        override suspend fun tracks(cursor: String?, limit: Int, query: TrackQuery): RemotePage<TrackSummaryDto> =
            error("unused")

        override suspend fun artists(cursor: String?, limit: Int, query: ArtistQuery): RemotePage<ArtistSummaryDto> =
            error("unused")

        override suspend fun albums(cursor: String?, limit: Int, query: AlbumQuery): RemotePage<AlbumSummaryDto> =
            error("unused")

        override suspend fun track(trackId: String): TrackDetailDto = error("unused")

        override suspend fun artist(artistId: String): ArtistDetailDto {
            artistDetailCalls += 1
            return checkNotNull(artistDetailResponse) { "Missing artistDetailResponse" }
        }

        override suspend fun album(albumId: String): AlbumDetailDto {
            albumDetailCalls += 1
            return checkNotNull(albumDetailResponse) { "Missing albumDetailResponse" }
        }
    }

    private companion object {
        const val ARTIST_ID = "11111111-1111-1111-1111-111111111111"
        const val ALBUM_ID_1 = "22222222-2222-2222-2222-222222222221"
        const val ALBUM_ID_2 = "22222222-2222-2222-2222-222222222222"
        const val ALBUM_ID_3 = "22222222-2222-2222-2222-222222222223"
        const val TRACK_ID_1 = "33333333-3333-3333-3333-333333333331"
        const val TRACK_ID_2 = "33333333-3333-3333-3333-333333333332"
    }
}
