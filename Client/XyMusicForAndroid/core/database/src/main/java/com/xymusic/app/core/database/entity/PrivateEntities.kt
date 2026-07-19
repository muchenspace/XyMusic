package com.xymusic.app.core.database.entity

import androidx.room.ColumnInfo
import androidx.room.Embedded
import androidx.room.Entity
import androidx.room.ForeignKey
import androidx.room.Index
import com.xymusic.app.core.database.model.PlaylistVisibility
import com.xymusic.app.core.database.model.SearchScope

@Entity(
    tableName = "favorites",
    primaryKeys = ["owner_user_id", "track_id"],
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
        Index(name = "index_favorites_track", value = ["track_id"]),
        Index(name = "index_favorites_owner_time", value = ["owner_user_id", "favorited_at_epoch_ms"]),
    ],
)
data class FavoriteEntity(
    @ColumnInfo(name = "owner_user_id") val ownerUserId: String,
    @ColumnInfo(name = "track_id") val trackId: String,
    @ColumnInfo(name = "favorited_at_epoch_ms") val favoritedAtEpochMs: Long,
)

@Entity(
    tableName = "playback_history",
    primaryKeys = ["owner_user_id", "track_id"],
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
        Index(name = "index_playback_history_track", value = ["track_id"]),
        Index(
            name = "index_playback_history_owner_last_played",
            value = ["owner_user_id", "last_played_at_epoch_ms"],
        ),
    ],
)
data class HistoryEntity(
    @ColumnInfo(name = "owner_user_id") val ownerUserId: String,
    @ColumnInfo(name = "track_id") val trackId: String,
    @ColumnInfo(name = "last_position_ms") val lastPositionMs: Long,
    @ColumnInfo(name = "play_count") val playCount: Long,
    @ColumnInfo(name = "last_played_at_epoch_ms") val lastPlayedAtEpochMs: Long,
    @ColumnInfo(name = "completed") val completed: Boolean,
    @ColumnInfo(name = "updated_at_epoch_ms") val updatedAtEpochMs: Long,
)

@Entity(
    tableName = "offline_tracks",
    primaryKeys = ["owner_user_id", "track_id"],
    indices = [
        Index(
            name = "index_offline_tracks_owner_downloaded_at",
            value = ["owner_user_id", "downloaded_at_epoch_ms"],
        ),
        Index(name = "index_offline_tracks_cache_key", value = ["cache_key"]),
    ],
)
data class OfflineTrackEntity(
    @ColumnInfo(name = "owner_user_id") val ownerUserId: String,
    @ColumnInfo(name = "track_id") val trackId: String,
    @ColumnInfo(name = "title") val title: String,
    @ColumnInfo(name = "artist_names_json") val artistNamesJson: String,
    @ColumnInfo(name = "album_title") val albumTitle: String?,
    @ColumnInfo(name = "artwork_url") val artworkUrl: String?,
    @ColumnInfo(name = "artwork_cache_key") val artworkCacheKey: String?,
    @ColumnInfo(name = "duration_ms") val durationMs: Long,
    @ColumnInfo(name = "cache_key") val cacheKey: String,
    @ColumnInfo(name = "content_length") val contentLength: Long,
    @ColumnInfo(name = "downloaded_at_epoch_ms") val downloadedAtEpochMs: Long,
)

@Entity(
    tableName = "playlists",
    primaryKeys = ["owner_user_id", "id"],
    indices = [
        Index(name = "index_playlists_id", value = ["id"]),
        Index(name = "index_playlists_owner_name", value = ["owner_user_id", "name"]),
        Index(name = "index_playlists_owner_updated", value = ["owner_user_id", "updated_at_epoch_ms"]),
    ],
)
data class PlaylistEntity(
    @ColumnInfo(name = "owner_user_id") val ownerUserId: String,
    @ColumnInfo(name = "id") val id: String,
    @ColumnInfo(name = "name") val name: String,
    @ColumnInfo(name = "description") val description: String?,
    @ColumnInfo(name = "visibility") val visibility: PlaylistVisibility,
    @Embedded(prefix = "cover_") val cover: ArtworkColumns?,
    @ColumnInfo(name = "track_count") val trackCount: Int,
    @ColumnInfo(name = "version") val version: Long,
    @ColumnInfo(name = "created_at_epoch_ms") val createdAtEpochMs: Long,
    @ColumnInfo(name = "updated_at_epoch_ms") val updatedAtEpochMs: Long,
)

@Entity(
    tableName = "playlist_entries",
    primaryKeys = ["owner_user_id", "id"],
    foreignKeys = [
        ForeignKey(
            entity = PlaylistEntity::class,
            parentColumns = ["owner_user_id", "id"],
            childColumns = ["owner_user_id", "playlist_id"],
            onDelete = ForeignKey.CASCADE,
            onUpdate = ForeignKey.CASCADE,
        ),
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
            name = "index_playlist_entries_playlist_position",
            value = ["owner_user_id", "playlist_id", "position"],
            unique = true,
        ),
        Index(name = "index_playlist_entries_track", value = ["track_id"]),
        Index(name = "index_playlist_entries_playlist", value = ["owner_user_id", "playlist_id"]),
    ],
)
data class PlaylistEntryEntity(
    @ColumnInfo(name = "owner_user_id") val ownerUserId: String,
    @ColumnInfo(name = "id") val id: String,
    @ColumnInfo(name = "playlist_id") val playlistId: String,
    @ColumnInfo(name = "position") val position: Int,
    @ColumnInfo(name = "track_id") val trackId: String,
    @ColumnInfo(name = "added_by_user_id") val addedByUserId: String,
    @ColumnInfo(name = "added_at_epoch_ms") val addedAtEpochMs: Long,
)

@Entity(
    tableName = "playback_queue",
    primaryKeys = ["owner_user_id", "item_id"],
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
            name = "index_playback_queue_owner_position",
            value = ["owner_user_id", "position"],
            unique = true,
        ),
        Index(name = "index_playback_queue_track", value = ["track_id"]),
    ],
)
data class PlaybackQueueEntity(
    @ColumnInfo(name = "owner_user_id") val ownerUserId: String,
    @ColumnInfo(name = "item_id") val itemId: String,
    @ColumnInfo(name = "position") val position: Int,
    @ColumnInfo(name = "track_id") val trackId: String,
    @ColumnInfo(name = "variant_id") val variantId: String?,
    @ColumnInfo(name = "stable_cache_key") val stableCacheKey: String?,
    @ColumnInfo(name = "resume_position_ms") val resumePositionMs: Long,
    @ColumnInfo(name = "is_current") val isCurrent: Boolean,
    @ColumnInfo(name = "enqueued_at_epoch_ms") val enqueuedAtEpochMs: Long,
    @ColumnInfo(name = "title", defaultValue = "''") val title: String = "",
    @ColumnInfo(name = "artist_names_json", defaultValue = "'[]'")
    val artistNamesJson: String = "[]",
    @ColumnInfo(name = "album_title") val albumTitle: String? = null,
    @ColumnInfo(name = "artwork_url") val artworkUrl: String? = null,
    @ColumnInfo(name = "artwork_cache_key") val artworkCacheKey: String? = null,
    @ColumnInfo(name = "duration_ms", defaultValue = "0") val durationMs: Long = 0,
)

@Entity(
    tableName = "search_history",
    primaryKeys = ["owner_user_id", "normalized_query", "scope"],
    indices = [
        Index(
            name = "index_search_history_owner_time",
            value = ["owner_user_id", "searched_at_epoch_ms"],
        ),
    ],
)
data class SearchHistoryEntity(
    @ColumnInfo(name = "owner_user_id") val ownerUserId: String,
    @ColumnInfo(name = "normalized_query") val normalizedQuery: String,
    @ColumnInfo(name = "scope") val scope: SearchScope,
    @ColumnInfo(name = "query") val query: String,
    @ColumnInfo(name = "searched_at_epoch_ms") val searchedAtEpochMs: Long,
)
