package com.xymusic.app.core.database

import android.app.Application
import androidx.paging.PagingSource
import androidx.room.Room
import androidx.room.withTransaction
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.database.entity.AlbumArtistCreditEntity
import com.xymusic.app.core.database.entity.AlbumEntity
import com.xymusic.app.core.database.entity.ArtistEntity
import com.xymusic.app.core.database.entity.CatalogRemoteKeyEntity
import com.xymusic.app.core.database.entity.FavoriteEntity
import com.xymusic.app.core.database.entity.LyricsEntity
import com.xymusic.app.core.database.entity.TrackArtistCreditEntity
import com.xymusic.app.core.database.entity.TrackEntity
import com.xymusic.app.core.database.model.ArtistCreditRole
import com.xymusic.app.core.database.model.CatalogItemType
import com.xymusic.app.core.database.model.LyricsFormat
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
class CatalogDaoReadModelsTest {
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
    fun pagedCollectionsFollowRemoteKeyOrderAndItemType() = runTest {
        val firstArtist = artist("artist-1")
        val secondArtist = artist("artist-2")
        database.catalogDao().upsertArtists(listOf(firstArtist, secondArtist))

        val album = album("album-1")
        database.catalogDao().replaceAlbum(
            album = album,
            credits = listOf(albumCredit(album.id, firstArtist.id)),
        )
        val firstTrack = track("track-1", album.id)
        val secondTrack = track("track-2", null)
        database.catalogDao().replaceTrackMetadata(
            firstTrack,
            listOf(trackCredit(firstTrack.id, firstArtist.id)),
        )
        database.catalogDao().replaceTrackMetadata(
            secondTrack,
            listOf(trackCredit(secondTrack.id, secondArtist.id)),
        )

        database.catalogRemoteKeyDao().replace(
            collectionKey = COLLECTION_KEY,
            itemType = CatalogItemType.TRACK,
            keys =
            listOf(
                remoteKey(CatalogItemType.TRACK, secondTrack.id, position = 0),
                remoteKey(CatalogItemType.TRACK, firstTrack.id, position = 1),
            ),
        )
        database.catalogRemoteKeyDao().replace(
            collectionKey = COLLECTION_KEY,
            itemType = CatalogItemType.ARTIST,
            keys =
            listOf(
                remoteKey(CatalogItemType.ARTIST, secondArtist.id, position = 0),
                remoteKey(CatalogItemType.ARTIST, firstArtist.id, position = 1),
            ),
        )
        database.catalogRemoteKeyDao().replace(
            collectionKey = COLLECTION_KEY,
            itemType = CatalogItemType.ALBUM,
            keys = listOf(remoteKey(CatalogItemType.ALBUM, album.id, position = 0)),
        )

        val tracks = database.catalogDao().pagedTracks(COLLECTION_KEY).refreshPage()
        val artists = database.catalogDao().pagedArtists(COLLECTION_KEY).refreshPage()
        val albums = database.catalogDao().pagedAlbums(COLLECTION_KEY).refreshPage()

        assertThat(tracks.map { it.track.id })
            .containsExactly(secondTrack.id, firstTrack.id)
            .inOrder()
        assertThat(artists.map(ArtistEntity::id))
            .containsExactly(secondArtist.id, firstArtist.id)
            .inOrder()
        assertThat(albums.map { it.album.id }).containsExactly(album.id)
        assertThat(tracks.first { it.track.id == firstTrack.id }.album?.id).isEqualTo(album.id)
        assertThat(
            tracks
                .first { it.track.id == firstTrack.id }
                .credits
                .single()
                .artistId,
        ).isEqualTo(firstArtist.id)
        assertThat(albums.single().artists.map(ArtistEntity::id)).containsExactly(firstArtist.id)
    }

    @Test
    fun detailReadModelsContainAlbumCreditsArtistsAndLyrics() = runTest {
        val primaryArtist = artist("artist-primary")
        val featuredArtist = artist("artist-featured")
        database.catalogDao().upsertArtists(listOf(primaryArtist, featuredArtist))
        val album = album("album-detail")
        database.catalogDao().replaceAlbum(
            album = album,
            credits =
            listOf(
                albumCredit(album.id, primaryArtist.id, sortOrder = 0),
                albumCredit(album.id, featuredArtist.id, sortOrder = 1),
            ),
        )
        val track = track("track-detail", album.id)
        database.catalogDao().replaceTrackMetadata(
            track = track,
            credits =
            listOf(
                trackCredit(track.id, primaryArtist.id, sortOrder = 0),
                trackCredit(track.id, featuredArtist.id, sortOrder = 1),
            ),
        )
        database.catalogDao().replaceLyrics(
            trackId = track.id,
            lyrics = listOf(lyrics("lyrics-default", track.id)),
        )

        val trackDetail = database.catalogDao().observeTrack(track.id).first()
        val albumDetail = database.catalogDao().observeAlbum(album.id).first()

        assertThat(trackDetail).isNotNull()
        assertThat(trackDetail!!.album?.id).isEqualTo(album.id)
        assertThat(trackDetail.credits.sortedBy { it.sortOrder }.map { it.artistId })
            .containsExactly(primaryArtist.id, featuredArtist.id)
            .inOrder()
        assertThat(trackDetail.artists.map(ArtistEntity::id))
            .containsExactly(primaryArtist.id, featuredArtist.id)
        assertThat(trackDetail.lyrics.map(LyricsEntity::id)).containsExactly("lyrics-default")
        assertThat(albumDetail).isNotNull()
        assertThat(albumDetail!!.credits.sortedBy { it.sortOrder }.map { it.artistId })
            .containsExactly(primaryArtist.id, featuredArtist.id)
            .inOrder()
        assertThat(albumDetail.artists.map(ArtistEntity::id))
            .containsExactly(primaryArtist.id, featuredArtist.id)
    }

    @Test
    fun favoritePagingAndBatchLookupReturnCompleteTrackRelations() = runTest {
        val artist = artist("artist-library")
        val album = album("album-library")
        val track = track("track-library", album.id)
        database.catalogDao().upsertArtists(listOf(artist))
        database.catalogDao().replaceAlbum(album, listOf(albumCredit(album.id, artist.id)))
        database.catalogDao().replaceTrackMetadata(track, listOf(trackCredit(track.id, artist.id)))
        database.libraryDao().upsertFavorite(FavoriteEntity("owner", track.id, 1_000))

        val favorite =
            database
                .libraryDao()
                .pagedFavoriteTracks("owner")
                .refreshPage()
                .single()
        val batchTrack = database.catalogDao().tracks(listOf(track.id)).single()

        assertThat(favorite.album?.id).isEqualTo(album.id)
        assertThat(favorite.artists.map(ArtistEntity::id)).containsExactly(artist.id)
        assertThat(batchTrack.album?.id).isEqualTo(album.id)
        assertThat(batchTrack.credits.single().artistId).isEqualTo(artist.id)
    }

    @Test
    fun batchedTrackLookupSupportsMoreThanOneSqliteParameterPage() = runTest {
        val tracks = List(1_001) { index -> track("large-playlist-track-$index", albumId = null) }
        database.withTransaction {
            tracks.forEach { item -> database.catalogDao().upsertTrack(item) }
        }

        val loaded = database.catalogDao().tracksInBatches(tracks.map(TrackEntity::id))

        assertThat(loaded.map { it.track.id })
            .containsExactlyElementsIn(tracks.map(TrackEntity::id))
    }

    @Test
    fun searchOverviewUsesRemoteOrderAndReturnsAtMostFiveItemsPerType() = runTest {
        val artists = List(6) { index -> artist("search-artist-$index") }
        val albums = List(6) { index -> album("search-album-$index") }
        val tracks = List(6) { index -> track("search-track-$index", albumId = null) }
        database.catalogDao().upsertArtists(artists)
        albums.forEach { database.catalogDao().replaceAlbum(it, credits = emptyList()) }
        tracks.forEach { database.catalogDao().replaceTrackMetadata(it, credits = emptyList()) }

        database.catalogRemoteKeyDao().replace(
            collectionKey = SEARCH_COLLECTION_KEY,
            itemType = CatalogItemType.TRACK,
            keys =
            tracks.reversed().mapIndexed { index, item ->
                remoteKey(
                    itemType = CatalogItemType.TRACK,
                    itemId = item.id,
                    position = index.toLong(),
                    collectionKey = SEARCH_COLLECTION_KEY,
                )
            },
        )
        database.catalogRemoteKeyDao().replace(
            collectionKey = SEARCH_COLLECTION_KEY,
            itemType = CatalogItemType.ARTIST,
            keys =
            artists.reversed().mapIndexed { index, item ->
                remoteKey(
                    itemType = CatalogItemType.ARTIST,
                    itemId = item.id,
                    position = index.toLong(),
                    collectionKey = SEARCH_COLLECTION_KEY,
                )
            },
        )
        database.catalogRemoteKeyDao().replace(
            collectionKey = SEARCH_COLLECTION_KEY,
            itemType = CatalogItemType.ALBUM,
            keys =
            albums.reversed().mapIndexed { index, item ->
                remoteKey(
                    itemType = CatalogItemType.ALBUM,
                    itemId = item.id,
                    position = index.toLong(),
                    collectionKey = SEARCH_COLLECTION_KEY,
                )
            },
        )

        val trackOverview =
            database
                .catalogDao()
                .observeSearchTrackOverview(SEARCH_COLLECTION_KEY)
                .first()
        val artistOverview =
            database
                .catalogDao()
                .observeSearchArtistOverview(SEARCH_COLLECTION_KEY)
                .first()
        val albumOverview =
            database
                .catalogDao()
                .observeSearchAlbumOverview(SEARCH_COLLECTION_KEY)
                .first()

        assertThat(trackOverview.map { it.track.id })
            .containsExactlyElementsIn(tracks.reversed().take(5).map(TrackEntity::id))
            .inOrder()
        assertThat(artistOverview.map(ArtistEntity::id))
            .containsExactlyElementsIn(artists.reversed().take(5).map(ArtistEntity::id))
            .inOrder()
        assertThat(albumOverview.map { it.album.id })
            .containsExactlyElementsIn(albums.reversed().take(5).map(AlbumEntity::id))
            .inOrder()
    }

    @Suppress("UNCHECKED_CAST")
    private suspend fun <T : Any> PagingSource<Int, T>.refreshPage(): List<T> {
        val result =
            load(
                PagingSource.LoadParams.Refresh<Int>(
                    key = null,
                    loadSize = 20,
                    placeholdersEnabled = false,
                ),
            )
        return (result as PagingSource.LoadResult.Page<Int, T>).data
    }

    private fun artist(id: String) = ArtistEntity(
        id = id,
        name = id,
        description = "Description for $id",
        artwork = null,
        cachedAtEpochMs = 1_000,
    )

    private fun album(id: String) = AlbumEntity(
        id = id,
        title = id,
        description = "Description for $id",
        releaseDateEpochMs = 1_000,
        trackCount = 1,
        cover = null,
        cachedAtEpochMs = 1_000,
    )

    private fun track(id: String, albumId: String?) = TrackEntity(
        id = id,
        albumId = albumId,
        title = id,
        durationMs = 180_000,
        trackNumber = 1,
        discNumber = 1,
        publishedAtEpochMs = 1_000,
        artwork = null,
        cachedAtEpochMs = 1_000,
    )

    private fun trackCredit(trackId: String, artistId: String, sortOrder: Int = 0) = TrackArtistCreditEntity(
        trackId = trackId,
        artistId = artistId,
        role = ArtistCreditRole.PRIMARY,
        sortOrder = sortOrder,
    )

    private fun albumCredit(albumId: String, artistId: String, sortOrder: Int = 0) = AlbumArtistCreditEntity(
        albumId = albumId,
        artistId = artistId,
        role = ArtistCreditRole.PRIMARY,
        sortOrder = sortOrder,
    )

    private fun lyrics(id: String, trackId: String) = LyricsEntity(
        id = id,
        trackId = trackId,
        language = "zh-CN",
        format = LyricsFormat.LRC,
        content = "[00:00.00]$trackId",
        isDefault = true,
        trackVersion = 1,
        updatedAtEpochMs = 1_000,
    )

    private fun remoteKey(
        itemType: CatalogItemType,
        itemId: String,
        position: Long,
        collectionKey: String = COLLECTION_KEY,
    ) = CatalogRemoteKeyEntity(
        collectionKey = collectionKey,
        itemType = itemType,
        itemId = itemId,
        position = position,
        previousCursor = null,
        nextCursor = null,
        refreshedAtEpochMs = 1_000,
    )

    private companion object {
        const val COLLECTION_KEY = "catalog:test"
        const val SEARCH_COLLECTION_KEY = "search:v1:test-query"
    }
}
