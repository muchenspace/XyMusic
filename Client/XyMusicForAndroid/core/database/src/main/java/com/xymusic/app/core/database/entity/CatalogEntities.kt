package com.xymusic.app.core.database.entity

import androidx.room.ColumnInfo
import androidx.room.Embedded
import androidx.room.Entity
import androidx.room.ForeignKey
import androidx.room.Index
import androidx.room.PrimaryKey
import com.xymusic.app.core.database.model.ArtistCreditRole
import com.xymusic.app.core.database.model.LyricsFormat

data class ArtworkColumns(
    @ColumnInfo(name = "asset_id") val assetId: String?,
    @ColumnInfo(name = "url") val url: String?,
    @ColumnInfo(name = "cache_key") val cacheKey: String?,
    @ColumnInfo(name = "mime_type") val mimeType: String?,
    @ColumnInfo(name = "expires_at_epoch_ms") val expiresAtEpochMs: Long?,
    @ColumnInfo(name = "width") val width: Int?,
    @ColumnInfo(name = "height") val height: Int?,
)

@Entity(
    tableName = "artists",
    indices = [Index(name = "index_artists_name", value = ["name"])],
)
data class ArtistEntity(
    @PrimaryKey
    @ColumnInfo(name = "id") val id: String,
    @ColumnInfo(name = "name") val name: String,
    @ColumnInfo(name = "description") val description: String?,
    @Embedded(prefix = "artwork_") val artwork: ArtworkColumns?,
    @ColumnInfo(name = "cached_at_epoch_ms") val cachedAtEpochMs: Long,
)

@Entity(
    tableName = "albums",
    indices = [
        Index(name = "index_albums_title", value = ["title"]),
        Index(name = "index_albums_release_date", value = ["release_date_epoch_ms"]),
    ],
)
data class AlbumEntity(
    @PrimaryKey
    @ColumnInfo(name = "id") val id: String,
    @ColumnInfo(name = "title") val title: String,
    @ColumnInfo(name = "description") val description: String?,
    @ColumnInfo(name = "release_date_epoch_ms") val releaseDateEpochMs: Long?,
    @ColumnInfo(name = "track_count") val trackCount: Int,
    @Embedded(prefix = "cover_") val cover: ArtworkColumns?,
    @ColumnInfo(name = "cached_at_epoch_ms") val cachedAtEpochMs: Long,
)

@Entity(
    tableName = "album_artist_credits",
    primaryKeys = ["album_id", "artist_id", "role"],
    foreignKeys = [
        ForeignKey(
            entity = AlbumEntity::class,
            parentColumns = ["id"],
            childColumns = ["album_id"],
            onDelete = ForeignKey.CASCADE,
            onUpdate = ForeignKey.CASCADE,
        ),
        ForeignKey(
            entity = ArtistEntity::class,
            parentColumns = ["id"],
            childColumns = ["artist_id"],
            onDelete = ForeignKey.CASCADE,
            onUpdate = ForeignKey.CASCADE,
        ),
    ],
    indices = [
        Index(name = "index_album_artist_credits_artist", value = ["artist_id"]),
        Index(
            name = "index_album_artist_credits_order",
            value = ["album_id", "sort_order"],
            unique = true,
        ),
    ],
)
data class AlbumArtistCreditEntity(
    @ColumnInfo(name = "album_id") val albumId: String,
    @ColumnInfo(name = "artist_id") val artistId: String,
    @ColumnInfo(name = "role") val role: ArtistCreditRole,
    @ColumnInfo(name = "sort_order") val sortOrder: Int,
)

@Entity(
    tableName = "tracks",
    foreignKeys = [
        ForeignKey(
            entity = AlbumEntity::class,
            parentColumns = ["id"],
            childColumns = ["album_id"],
            onDelete = ForeignKey.SET_NULL,
            onUpdate = ForeignKey.CASCADE,
        ),
    ],
    indices = [
        Index(name = "index_tracks_title", value = ["title"]),
        Index(name = "index_tracks_published_at", value = ["published_at_epoch_ms"]),
        Index(
            name = "index_tracks_album_order",
            value = ["album_id", "disc_number", "track_number"],
        ),
    ],
)
data class TrackEntity(
    @PrimaryKey
    @ColumnInfo(name = "id") val id: String,
    @ColumnInfo(name = "album_id") val albumId: String?,
    @ColumnInfo(name = "title") val title: String,
    @ColumnInfo(name = "duration_ms") val durationMs: Long,
    @ColumnInfo(name = "track_number") val trackNumber: Int?,
    @ColumnInfo(name = "disc_number") val discNumber: Int,
    @ColumnInfo(name = "published_at_epoch_ms") val publishedAtEpochMs: Long,
    @Embedded(prefix = "artwork_") val artwork: ArtworkColumns?,
    @ColumnInfo(name = "cached_at_epoch_ms") val cachedAtEpochMs: Long,
)

@Entity(
    tableName = "track_artist_credits",
    primaryKeys = ["track_id", "artist_id", "role"],
    foreignKeys = [
        ForeignKey(
            entity = TrackEntity::class,
            parentColumns = ["id"],
            childColumns = ["track_id"],
            onDelete = ForeignKey.CASCADE,
            onUpdate = ForeignKey.CASCADE,
        ),
        ForeignKey(
            entity = ArtistEntity::class,
            parentColumns = ["id"],
            childColumns = ["artist_id"],
            onDelete = ForeignKey.CASCADE,
            onUpdate = ForeignKey.CASCADE,
        ),
    ],
    indices = [
        Index(name = "index_track_artist_credits_artist", value = ["artist_id"]),
        Index(
            name = "index_track_artist_credits_order",
            value = ["track_id", "sort_order"],
            unique = true,
        ),
    ],
)
data class TrackArtistCreditEntity(
    @ColumnInfo(name = "track_id") val trackId: String,
    @ColumnInfo(name = "artist_id") val artistId: String,
    @ColumnInfo(name = "role") val role: ArtistCreditRole,
    @ColumnInfo(name = "sort_order") val sortOrder: Int,
)

@Entity(
    tableName = "lyrics",
    foreignKeys = [
        ForeignKey(
            entity = TrackEntity::class,
            parentColumns = ["id"],
            childColumns = ["track_id"],
            onDelete = ForeignKey.CASCADE,
            onUpdate = ForeignKey.CASCADE,
        ),
    ],
    indices = [
        Index(
            name = "index_lyrics_track_language_format",
            value = ["track_id", "language", "format"],
            unique = true,
        ),
        Index(name = "index_lyrics_track_default", value = ["track_id", "is_default"]),
    ],
)
data class LyricsEntity(
    @PrimaryKey
    @ColumnInfo(name = "id") val id: String,
    @ColumnInfo(name = "track_id") val trackId: String,
    @ColumnInfo(name = "language") val language: String,
    @ColumnInfo(name = "format") val format: LyricsFormat,
    @ColumnInfo(name = "content") val content: String,
    @ColumnInfo(name = "is_default") val isDefault: Boolean,
    @ColumnInfo(name = "track_version") val trackVersion: Long,
    @ColumnInfo(name = "updated_at_epoch_ms") val updatedAtEpochMs: Long,
)
