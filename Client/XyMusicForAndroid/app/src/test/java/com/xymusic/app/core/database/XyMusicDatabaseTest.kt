package com.xymusic.app.core.database

import android.app.Application
import android.database.sqlite.SQLiteConstraintException
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.database.entity.ArtistEntity
import com.xymusic.app.core.database.entity.CatalogRemoteKeyEntity
import com.xymusic.app.core.database.entity.FavoriteEntity
import com.xymusic.app.core.database.entity.HistoryEntity
import com.xymusic.app.core.database.entity.LyricsEntity
import com.xymusic.app.core.database.entity.OfflineTrackEntity
import com.xymusic.app.core.database.entity.PlaybackQueueEntity
import com.xymusic.app.core.database.entity.PlaylistEntity
import com.xymusic.app.core.database.entity.PlaylistEntryEntity
import com.xymusic.app.core.database.entity.SearchHistoryEntity
import com.xymusic.app.core.database.entity.TrackArtistCreditEntity
import com.xymusic.app.core.database.entity.TrackEntity
import com.xymusic.app.core.database.model.ArtistCreditRole
import com.xymusic.app.core.database.model.CatalogItemType
import com.xymusic.app.core.database.model.LyricsFormat
import com.xymusic.app.core.database.model.PlaylistVisibility
import com.xymusic.app.core.database.model.SearchScope
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
class XyMusicDatabaseTest {
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
    fun trackMetadataRefreshPreservesLyricsUntilLyricsAreExplicitlyReplaced() = runTest {
        val firstArtist = artist("artist-1")
        val secondArtist = artist("artist-2")
        database.catalogDao().upsertArtists(listOf(firstArtist, secondArtist))
        val track = track("track-1")

        database.catalogDao().replaceTrackMetadata(
            track = track,
            credits = listOf(credit(track.id, firstArtist.id)),
        )
        database.catalogDao().replaceLyrics(
            trackId = track.id,
            lyrics = listOf(lyrics("lyrics-1", track.id, "first")),
        )
        database.catalogDao().replaceTrackMetadata(
            track = track.copy(title = "Updated"),
            credits = listOf(credit(track.id, secondArtist.id)),
        )

        assertThat(database.catalogDao().track(track.id)?.title).isEqualTo("Updated")
        assertThat(database.catalogDao().artistsForTrack(track.id).map { it.id })
            .containsExactly(secondArtist.id)
        assertThat(
            database
                .catalogDao()
                .observeLyrics(track.id)
                .first()
                .map { it.id },
        ).containsExactly("lyrics-1")

        database.catalogDao().replaceLyrics(
            trackId = track.id,
            lyrics = listOf(lyrics("lyrics-2", track.id, "second")),
        )

        assertThat(
            database
                .catalogDao()
                .observeLyrics(track.id)
                .first()
                .map { it.id },
        ).containsExactly("lyrics-2")
    }

    @Test
    fun snapshotReplacementRejectsDuplicateStableIdsBeforeWriting() = runTest {
        database.seedTrack("track-1")
        database.seedTrack("track-2")
        val playlist = playlist("alice", "playlist-1")

        val playlistFailure =
            runCatching {
                database.playlistDao().replacePlaylist(
                    playlist,
                    listOf(
                        entry("alice", playlist.id, "entry-1", "track-1", 0),
                        entry("alice", playlist.id, "entry-1", "track-2", 1),
                    ),
                )
            }.exceptionOrNull()
        val queueFailure =
            runCatching {
                database.playbackQueueDao().replace(
                    "alice",
                    listOf(
                        queueItem("alice", "item-1", "track-1", 0, true),
                        queueItem("alice", "item-1", "track-2", 1, false),
                    ),
                )
            }.exceptionOrNull()
        val remoteKeyFailure =
            runCatching {
                database.catalogRemoteKeyDao().replace(
                    "home",
                    CatalogItemType.TRACK,
                    listOf(remoteKey("track-1", 0), remoteKey("track-1", 1)),
                )
            }.exceptionOrNull()

        assertThat(playlistFailure).isInstanceOf(IllegalArgumentException::class.java)
        assertThat(queueFailure).isInstanceOf(IllegalArgumentException::class.java)
        assertThat(remoteKeyFailure).isInstanceOf(IllegalArgumentException::class.java)
        assertThat(database.playlistDao().entries("alice", playlist.id)).isEmpty()
        assertThat(database.playbackQueueDao().observe("alice").first()).isEmpty()
    }

    @Test
    fun ownerScopedQueriesNeverExposeAnotherAccount() = runTest {
        database.seedTrack("track-1")
        database.libraryDao().upsertFavorite(FavoriteEntity("alice", "track-1", 1_000))
        database.libraryDao().upsertFavorite(FavoriteEntity("bob", "track-1", 2_000))
        database.libraryDao().upsertHistory(history("alice", "track-1", 1_000))
        database.libraryDao().upsertHistory(history("bob", "track-1", 2_000))

        assertThat(database.libraryDao().observeFavorite("alice", "track-1").first()).isTrue()
        assertThat(
            database
                .libraryDao()
                .observeHistory("alice")
                .first()
                .map { it.ownerUserId },
        ).containsExactly("alice")

        database.libraryDao().deleteFavorite("alice", "track-1")

        assertThat(database.libraryDao().observeFavorite("alice", "track-1").first()).isFalse()
        assertThat(database.libraryDao().observeFavorite("bob", "track-1").first()).isTrue()
        assertThat(
            database
                .libraryDao()
                .observeHistory("bob")
                .first()
                .map { it.ownerUserId },
        ).containsExactly("bob")
    }

    @Test
    fun offlineTracksAreOwnerScopedAndMayShareCachedMedia() = runTest {
        val offline =
            OfflineTrackEntity(
                ownerUserId = "alice",
                trackId = "track-offline",
                title = "Offline track",
                artistNamesJson = "[\"Artist\"]",
                albumTitle = "Album",
                artworkUrl = null,
                artworkCacheKey = null,
                durationMs = 180_000,
                cacheKey = "offline-cache-key",
                contentLength = 1_024,
                downloadedAtEpochMs = 10_000,
            )
        val sharedOffline = offline.copy(ownerUserId = "bob")

        database.offlineTrackDao().upsert(offline)
        database.offlineTrackDao().upsert(sharedOffline)

        assertThat(
            database.offlineTrackDao().observeDownloaded("alice", offline.trackId).first(),
        ).isTrue()
        assertThat(database.offlineTrackDao().observeAll("alice").first()).containsExactly(offline)
        assertThat(database.offlineTrackDao().observeAll("bob").first())
            .containsExactly(sharedOffline)
        assertThat(database.offlineTrackDao().cacheKeyReferenceCount(offline.cacheKey)).isEqualTo(2)

        database.offlineTrackDao().delete("alice", offline.trackId)

        assertThat(
            database.offlineTrackDao().observeDownloaded("alice", offline.trackId).first(),
        ).isFalse()
        assertThat(
            database.offlineTrackDao().observeDownloaded("bob", offline.trackId).first(),
        ).isTrue()
        assertThat(database.offlineTrackDao().cacheKeyReferenceCount(offline.cacheKey)).isEqualTo(1)
    }

    @Test
    fun playlistReplacementRollsBackWhenAChildViolatesForeignKey() = runTest {
        database.seedTrack("track-1")
        val playlist = playlist("alice", "playlist-1")
        val originalEntry = entry("alice", playlist.id, "entry-1", "track-1", 0)
        database.playlistDao().replacePlaylist(playlist, listOf(originalEntry))

        val failure =
            runCatching {
                database.playlistDao().replaceEntries(
                    ownerUserId = "alice",
                    playlistId = playlist.id,
                    entries = listOf(entry("alice", playlist.id, "entry-invalid", "missing-track", 0)),
                )
            }.exceptionOrNull()

        assertThat(failure).isInstanceOf(SQLiteConstraintException::class.java)
        assertThat(database.playlistDao().entries("alice", playlist.id))
            .containsExactly(originalEntry)
    }

    @Test
    fun queueAndSearchTransactionsValidateAndTrimSnapshots() = runTest {
        database.seedTrack("track-1")
        database.seedTrack("track-2")
        val first = queueItem("alice", "item-1", "track-1", 0, isCurrent = true)
        val second = queueItem("alice", "item-2", "track-2", 1, isCurrent = false)
        database.playbackQueueDao().replace("alice", listOf(first, second))

        assertThat(database.playbackQueueDao().observe("alice").first()).containsExactly(first, second).inOrder()
        assertThat(database.playbackQueueDao().current("alice")).isEqualTo(first)

        repeat(3) { index ->
            database.searchHistoryDao().record(
                SearchHistoryEntity(
                    ownerUserId = "alice",
                    normalizedQuery = "query-$index",
                    scope = SearchScope.ALL,
                    query = "Query $index",
                    searchedAtEpochMs = index.toLong(),
                ),
                limit = 2,
            )
        }

        assertThat(
            database
                .searchHistoryDao()
                .observe("alice")
                .first()
                .map { it.normalizedQuery },
        ).containsExactly("query-2", "query-1")
            .inOrder()
    }

    private fun artist(id: String) = ArtistEntity(
        id = id,
        name = id,
        description = null,
        artwork = null,
        cachedAtEpochMs = 1_000,
    )

    private fun track(id: String) = TrackEntity(
        id = id,
        albumId = null,
        title = id,
        durationMs = 180_000,
        trackNumber = 1,
        discNumber = 1,
        publishedAtEpochMs = 1_000,
        artwork = null,
        cachedAtEpochMs = 1_000,
    )

    private fun credit(trackId: String, artistId: String) = TrackArtistCreditEntity(
        trackId = trackId,
        artistId = artistId,
        role = ArtistCreditRole.PRIMARY,
        sortOrder = 0,
    )

    private fun lyrics(id: String, trackId: String, content: String) = LyricsEntity(
        id = id,
        trackId = trackId,
        language = "zh-CN",
        format = LyricsFormat.LRC,
        content = content,
        isDefault = true,
        trackVersion = 1,
        updatedAtEpochMs = 1_000,
    )

    private fun history(owner: String, trackId: String, time: Long) = HistoryEntity(
        ownerUserId = owner,
        trackId = trackId,
        lastPositionMs = 500,
        playCount = 1,
        lastPlayedAtEpochMs = time,
        completed = false,
        updatedAtEpochMs = time,
    )

    private fun playlist(owner: String, id: String) = PlaylistEntity(
        ownerUserId = owner,
        id = id,
        name = id,
        description = null,
        visibility = PlaylistVisibility.PRIVATE,
        cover = null,
        trackCount = 1,
        version = 1,
        createdAtEpochMs = 1_000,
        updatedAtEpochMs = 1_000,
    )

    private fun entry(owner: String, playlistId: String, id: String, trackId: String, position: Int) =
        PlaylistEntryEntity(
            ownerUserId = owner,
            id = id,
            playlistId = playlistId,
            position = position,
            trackId = trackId,
            addedByUserId = owner,
            addedAtEpochMs = 1_000,
        )

    private fun queueItem(owner: String, id: String, trackId: String, position: Int, isCurrent: Boolean) =
        PlaybackQueueEntity(
            ownerUserId = owner,
            itemId = id,
            position = position,
            trackId = trackId,
            variantId = "variant-$trackId",
            stableCacheKey = "cache-$trackId-v1",
            resumePositionMs = 0,
            isCurrent = isCurrent,
            enqueuedAtEpochMs = 1_000,
            title = "Title $trackId",
            artistNamesJson = "[\"Artist\"]",
            albumTitle = "Album",
            artworkUrl = "https://cdn.test/$trackId.jpg",
            artworkCacheKey = "artwork-$trackId-v1",
            durationMs = 180_000,
        )

    private fun remoteKey(itemId: String, position: Long) = CatalogRemoteKeyEntity(
        collectionKey = "home",
        itemType = CatalogItemType.TRACK,
        itemId = itemId,
        position = position,
        previousCursor = null,
        nextCursor = null,
        refreshedAtEpochMs = 1_000,
    )
}
