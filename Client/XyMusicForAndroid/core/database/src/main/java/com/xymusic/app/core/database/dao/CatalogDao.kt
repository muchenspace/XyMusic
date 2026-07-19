package com.xymusic.app.core.database.dao

import androidx.paging.PagingSource
import androidx.room.Dao
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import androidx.room.Transaction
import androidx.room.Upsert
import com.xymusic.app.core.database.entity.AlbumArtistCreditEntity
import com.xymusic.app.core.database.entity.AlbumEntity
import com.xymusic.app.core.database.entity.ArtistEntity
import com.xymusic.app.core.database.entity.LyricsEntity
import com.xymusic.app.core.database.entity.TrackArtistCreditEntity
import com.xymusic.app.core.database.entity.TrackEntity
import com.xymusic.app.core.database.model.AlbumReadModel
import com.xymusic.app.core.database.model.TrackDetailReadModel
import com.xymusic.app.core.database.model.TrackSummaryReadModel
import kotlinx.coroutines.flow.Flow

@Dao
abstract class CatalogDao {
    @Upsert
    abstract suspend fun upsertArtists(artists: List<ArtistEntity>)

    @Insert(onConflict = OnConflictStrategy.IGNORE)
    protected abstract suspend fun insertArtistsIfMissing(artists: List<ArtistEntity>)

    @Query(
        """
        UPDATE artists SET
            name = :name,
            cached_at_epoch_ms = :cachedAtEpochMs
        WHERE id = :id
        """,
    )
    protected abstract suspend fun updateArtistReference(id: String, name: String, cachedAtEpochMs: Long)

    @Query(
        """
        UPDATE artists SET
            name = :name,
            artwork_asset_id = :assetId,
            artwork_url = :url,
            artwork_cache_key = :cacheKey,
            artwork_mime_type = :mimeType,
            artwork_expires_at_epoch_ms = :expiresAtEpochMs,
            artwork_width = :width,
            artwork_height = :height,
            cached_at_epoch_ms = :cachedAtEpochMs
        WHERE id = :id
        """,
    )
    protected abstract suspend fun updateArtistSummary(
        id: String,
        name: String,
        assetId: String?,
        url: String?,
        cacheKey: String?,
        mimeType: String?,
        expiresAtEpochMs: Long?,
        width: Int?,
        height: Int?,
        cachedAtEpochMs: Long,
    )

    @Upsert
    abstract suspend fun upsertAlbum(album: AlbumEntity)

    @Insert(onConflict = OnConflictStrategy.IGNORE)
    protected abstract suspend fun insertAlbumsIfMissing(albums: List<AlbumEntity>)

    @Query(
        """
        UPDATE albums SET
            title = :title,
            cached_at_epoch_ms = :cachedAtEpochMs
        WHERE id = :id
        """,
    )
    protected abstract suspend fun updateAlbumReference(id: String, title: String, cachedAtEpochMs: Long)

    @Query(
        """
        UPDATE albums SET
            title = :title,
            release_date_epoch_ms = :releaseDateEpochMs,
            track_count = :trackCount,
            cover_asset_id = :assetId,
            cover_url = :url,
            cover_cache_key = :cacheKey,
            cover_mime_type = :mimeType,
            cover_expires_at_epoch_ms = :expiresAtEpochMs,
            cover_width = :width,
            cover_height = :height,
            cached_at_epoch_ms = :cachedAtEpochMs
        WHERE id = :id
        """,
    )
    protected abstract suspend fun updateAlbumSummary(
        id: String,
        title: String,
        releaseDateEpochMs: Long?,
        trackCount: Int,
        assetId: String?,
        url: String?,
        cacheKey: String?,
        mimeType: String?,
        expiresAtEpochMs: Long?,
        width: Int?,
        height: Int?,
        cachedAtEpochMs: Long,
    )

    @Insert(onConflict = OnConflictStrategy.ABORT)
    protected abstract suspend fun insertAlbumCredits(credits: List<AlbumArtistCreditEntity>)

    @Query("DELETE FROM album_artist_credits WHERE album_id = :albumId")
    protected abstract suspend fun deleteAlbumCredits(albumId: String)

    @Query("DELETE FROM album_artist_credits WHERE album_id IN (:albumIds)")
    protected abstract suspend fun deleteAlbumCredits(albumIds: List<String>)

    @Upsert
    abstract suspend fun upsertTrack(track: TrackEntity)

    @Upsert
    protected abstract suspend fun upsertTracks(tracks: List<TrackEntity>)

    @Insert(onConflict = OnConflictStrategy.ABORT)
    protected abstract suspend fun insertTrackCredits(credits: List<TrackArtistCreditEntity>)

    @Query("DELETE FROM track_artist_credits WHERE track_id = :trackId")
    protected abstract suspend fun deleteTrackCredits(trackId: String)

    @Query("DELETE FROM track_artist_credits WHERE track_id IN (:trackIds)")
    protected abstract suspend fun deleteTrackCredits(trackIds: List<String>)

    @Upsert
    protected abstract suspend fun upsertLyrics(lyrics: List<LyricsEntity>)

    @Query("DELETE FROM lyrics WHERE track_id = :trackId")
    protected abstract suspend fun deleteLyrics(trackId: String)

    @Transaction
    open suspend fun mergeArtistReferences(artists: List<ArtistEntity>) {
        if (artists.isEmpty()) return
        insertArtistsIfMissing(artists)
        artists.forEach { artist ->
            updateArtistReference(artist.id, artist.name, artist.cachedAtEpochMs)
        }
    }

    @Transaction
    open suspend fun mergeArtistSummaries(artists: List<ArtistEntity>) {
        if (artists.isEmpty()) return
        insertArtistsIfMissing(artists)
        artists.forEach { artist ->
            updateArtistSummary(
                id = artist.id,
                name = artist.name,
                assetId = artist.artwork?.assetId,
                url = artist.artwork?.url,
                cacheKey = artist.artwork?.cacheKey,
                mimeType = artist.artwork?.mimeType,
                expiresAtEpochMs = artist.artwork?.expiresAtEpochMs,
                width = artist.artwork?.width,
                height = artist.artwork?.height,
                cachedAtEpochMs = artist.cachedAtEpochMs,
            )
        }
    }

    @Transaction
    open suspend fun mergeAlbumReferences(albums: List<AlbumEntity>) {
        if (albums.isEmpty()) return
        insertAlbumsIfMissing(albums)
        albums.forEach { album ->
            updateAlbumReference(album.id, album.title, album.cachedAtEpochMs)
        }
    }

    @Transaction
    open suspend fun mergeAlbumSummaries(albums: List<AlbumEntity>) {
        if (albums.isEmpty()) return
        insertAlbumsIfMissing(albums)
        albums.forEach { album ->
            updateAlbumSummary(
                id = album.id,
                title = album.title,
                releaseDateEpochMs = album.releaseDateEpochMs,
                trackCount = album.trackCount,
                assetId = album.cover?.assetId,
                url = album.cover?.url,
                cacheKey = album.cover?.cacheKey,
                mimeType = album.cover?.mimeType,
                expiresAtEpochMs = album.cover?.expiresAtEpochMs,
                width = album.cover?.width,
                height = album.cover?.height,
                cachedAtEpochMs = album.cachedAtEpochMs,
            )
        }
    }

    @Transaction
    open suspend fun mergeAlbumSummary(album: AlbumEntity, credits: List<AlbumArtistCreditEntity>) {
        require(credits.all { it.albumId == album.id }) { "Album credit belongs to a different album" }
        mergeAlbumSummaries(listOf(album))
        deleteAlbumCredits(album.id)
        if (credits.isNotEmpty()) insertAlbumCredits(credits)
    }

    @Transaction
    open suspend fun mergeAlbumSummaries(albums: List<AlbumEntity>, credits: List<AlbumArtistCreditEntity>) {
        if (albums.isEmpty()) {
            require(credits.isEmpty()) { "Album credits require an album" }
            return
        }
        val albumIds = albums.map(AlbumEntity::id)
        require(albumIds.distinct().size == albumIds.size) { "Album IDs must be unique" }
        require(credits.all { it.albumId in albumIds }) { "Album credit belongs to another batch" }
        require(credits.map { Triple(it.albumId, it.artistId, it.role) }.distinct().size == credits.size) {
            "Album credits must be unique"
        }
        mergeAlbumSummaries(albums)
        deleteAlbumCredits(albumIds)
        if (credits.isNotEmpty()) insertAlbumCredits(credits)
    }

    @Transaction
    open suspend fun replaceAlbum(album: AlbumEntity, credits: List<AlbumArtistCreditEntity>) {
        require(credits.all { it.albumId == album.id }) { "Album credit belongs to a different album" }
        upsertAlbum(album)
        deleteAlbumCredits(album.id)
        if (credits.isNotEmpty()) insertAlbumCredits(credits)
    }

    @Transaction
    open suspend fun replaceTrackMetadata(track: TrackEntity, credits: List<TrackArtistCreditEntity>) {
        require(credits.all { it.trackId == track.id }) { "Track credit belongs to a different track" }
        upsertTrack(track)
        deleteTrackCredits(track.id)
        if (credits.isNotEmpty()) insertTrackCredits(credits)
    }

    @Transaction
    open suspend fun replaceTrackMetadata(tracks: List<TrackEntity>, credits: List<TrackArtistCreditEntity>) {
        if (tracks.isEmpty()) {
            require(credits.isEmpty()) { "Track credits require a track" }
            return
        }
        val trackIds = tracks.map(TrackEntity::id)
        require(trackIds.distinct().size == trackIds.size) { "Track IDs must be unique" }
        require(credits.all { it.trackId in trackIds }) { "Track credit belongs to another batch" }
        require(credits.map { Triple(it.trackId, it.artistId, it.role) }.distinct().size == credits.size) {
            "Track credits must be unique"
        }
        upsertTracks(tracks)
        deleteTrackCredits(trackIds)
        if (credits.isNotEmpty()) insertTrackCredits(credits)
    }

    @Transaction
    open suspend fun replaceLyrics(trackId: String, lyrics: List<LyricsEntity>) {
        require(lyrics.all { it.trackId == trackId }) { "Lyrics belong to a different track" }
        deleteLyrics(trackId)
        if (lyrics.isNotEmpty()) upsertLyrics(lyrics)
    }

    @Query("SELECT * FROM artists WHERE id = :artistId")
    abstract suspend fun artist(artistId: String): ArtistEntity?

    @Query("SELECT * FROM artists WHERE id = :artistId")
    abstract fun observeArtist(artistId: String): Flow<ArtistEntity?>

    @Query("SELECT * FROM albums WHERE id = :albumId")
    abstract suspend fun album(albumId: String): AlbumEntity?

    @Transaction
    @Query("SELECT * FROM albums WHERE id = :albumId")
    abstract fun observeAlbum(albumId: String): Flow<AlbumReadModel?>

    @Query("SELECT * FROM tracks WHERE id = :trackId")
    abstract suspend fun track(trackId: String): TrackEntity?

    @Transaction
    @Query("SELECT * FROM tracks WHERE id IN (:trackIds)")
    abstract suspend fun tracks(trackIds: List<String>): List<TrackSummaryReadModel>

    @Transaction
    open suspend fun tracksInBatches(trackIds: List<String>): List<TrackSummaryReadModel> {
        if (trackIds.isEmpty()) return emptyList()
        return trackIds
            .distinct()
            .chunked(SQLITE_SAFE_QUERY_PARAMETER_COUNT)
            .flatMap { batch -> tracks(batch) }
    }

    @Transaction
    @Query("SELECT * FROM tracks WHERE id = :trackId")
    abstract fun observeTrack(trackId: String): Flow<TrackDetailReadModel?>

    @Query("SELECT * FROM lyrics WHERE track_id = :trackId ORDER BY is_default DESC, language, format")
    abstract fun observeLyrics(trackId: String): Flow<List<LyricsEntity>>

    @Query(
        """
        SELECT artists.* FROM artists
        INNER JOIN track_artist_credits ON artists.id = track_artist_credits.artist_id
        WHERE track_artist_credits.track_id = :trackId
        ORDER BY track_artist_credits.sort_order
        """,
    )
    abstract suspend fun artistsForTrack(trackId: String): List<ArtistEntity>

    @Query(
        """
        SELECT artists.* FROM artists
        INNER JOIN album_artist_credits ON artists.id = album_artist_credits.artist_id
        WHERE album_artist_credits.album_id = :albumId
        ORDER BY album_artist_credits.sort_order
        """,
    )
    abstract suspend fun artistsForAlbum(albumId: String): List<ArtistEntity>

    @Transaction
    @Query(
        """
        SELECT tracks.* FROM tracks
        INNER JOIN catalog_remote_keys AS remote_key
            ON remote_key.item_id = tracks.id
            AND remote_key.item_type = 'TRACK'
        WHERE remote_key.collection_key = :collectionKey
        ORDER BY remote_key.position ASC
        """,
    )
    abstract fun pagedTracks(collectionKey: String): PagingSource<Int, TrackSummaryReadModel>

    @Transaction
    @Query(
        """
        SELECT artists.* FROM artists
        INNER JOIN catalog_remote_keys AS remote_key
            ON remote_key.item_id = artists.id
            AND remote_key.item_type = 'ARTIST'
        WHERE remote_key.collection_key = :collectionKey
        ORDER BY remote_key.position ASC
        """,
    )
    abstract fun pagedArtists(collectionKey: String): PagingSource<Int, ArtistEntity>

    @Transaction
    @Query(
        """
        SELECT albums.* FROM albums
        INNER JOIN catalog_remote_keys AS remote_key
            ON remote_key.item_id = albums.id
            AND remote_key.item_type = 'ALBUM'
        WHERE remote_key.collection_key = :collectionKey
        ORDER BY remote_key.position ASC
        """,
    )
    abstract fun pagedAlbums(collectionKey: String): PagingSource<Int, AlbumReadModel>

    @Transaction
    @Query(
        """
        SELECT tracks.* FROM tracks
        INNER JOIN catalog_remote_keys AS remote_key
            ON remote_key.item_id = tracks.id
            AND remote_key.item_type = 'TRACK'
        WHERE remote_key.collection_key = :collectionKey
        ORDER BY remote_key.position ASC
        LIMIT 5
        """,
    )
    abstract fun observeSearchTrackOverview(collectionKey: String): Flow<List<TrackSummaryReadModel>>

    @Transaction
    @Query(
        """
        SELECT artists.* FROM artists
        INNER JOIN catalog_remote_keys AS remote_key
            ON remote_key.item_id = artists.id
            AND remote_key.item_type = 'ARTIST'
        WHERE remote_key.collection_key = :collectionKey
        ORDER BY remote_key.position ASC
        LIMIT 5
        """,
    )
    abstract fun observeSearchArtistOverview(collectionKey: String): Flow<List<ArtistEntity>>

    @Transaction
    @Query(
        """
        SELECT albums.* FROM albums
        INNER JOIN catalog_remote_keys AS remote_key
            ON remote_key.item_id = albums.id
            AND remote_key.item_type = 'ALBUM'
        WHERE remote_key.collection_key = :collectionKey
        ORDER BY remote_key.position ASC
        LIMIT 5
        """,
    )
    abstract fun observeSearchAlbumOverview(collectionKey: String): Flow<List<AlbumReadModel>>

    private companion object {
        const val SQLITE_SAFE_QUERY_PARAMETER_COUNT = 900
    }
}
