package com.xymusic.app.core.database.dao

import androidx.room.Dao
import androidx.room.Query
import androidx.room.Transaction
import androidx.room.Upsert
import com.xymusic.app.core.database.entity.PlaylistEntity
import com.xymusic.app.core.database.entity.PlaylistEntryEntity
import java.util.HashSet
import kotlinx.coroutines.flow.Flow

data class PlaylistSnapshot(val playlist: PlaylistEntity, val entries: List<PlaylistEntryEntity>)

@Dao
abstract class PlaylistDao {
    @Upsert
    abstract suspend fun upsertPlaylist(playlist: PlaylistEntity)

    @Upsert
    abstract suspend fun upsertPlaylists(playlists: List<PlaylistEntity>)

    @Upsert
    protected abstract suspend fun upsertEntries(entries: List<PlaylistEntryEntity>)

    @Query("DELETE FROM playlist_entries WHERE owner_user_id = :ownerUserId AND playlist_id = :playlistId")
    protected abstract suspend fun deleteEntries(ownerUserId: String, playlistId: String)

    @Query("DELETE FROM playlist_entries WHERE owner_user_id = :ownerUserId AND playlist_id IN (:playlistIds)")
    abstract suspend fun deleteEntriesForPlaylists(ownerUserId: String, playlistIds: List<String>): Int

    @Transaction
    open suspend fun replacePlaylist(playlist: PlaylistEntity, entries: List<PlaylistEntryEntity>) {
        validateEntries(playlist.ownerUserId, playlist.id, entries)
        upsertPlaylist(playlist)
        deleteEntries(playlist.ownerUserId, playlist.id)
        if (entries.isNotEmpty()) upsertEntries(entries)
    }

    @Transaction
    open suspend fun replaceEntries(ownerUserId: String, playlistId: String, entries: List<PlaylistEntryEntity>) {
        validateEntries(ownerUserId, playlistId, entries)
        deleteEntries(ownerUserId, playlistId)
        if (entries.isNotEmpty()) upsertEntries(entries)
    }

    @Query("SELECT * FROM playlists WHERE owner_user_id = :ownerUserId ORDER BY updated_at_epoch_ms DESC, id")
    abstract fun observePlaylists(ownerUserId: String): Flow<List<PlaylistEntity>>

    @Query("SELECT * FROM playlists WHERE owner_user_id = :ownerUserId ORDER BY updated_at_epoch_ms DESC, id")
    abstract suspend fun playlists(ownerUserId: String): List<PlaylistEntity>

    @Query("SELECT * FROM playlists WHERE owner_user_id = :ownerUserId AND id = :playlistId")
    abstract fun observePlaylistEntity(ownerUserId: String, playlistId: String): Flow<PlaylistEntity?>

    @Query("SELECT * FROM playlists WHERE owner_user_id = :ownerUserId AND id = :playlistId")
    abstract suspend fun playlist(ownerUserId: String, playlistId: String): PlaylistEntity?

    @Query(
        """
        SELECT * FROM playlist_entries
        WHERE owner_user_id = :ownerUserId AND playlist_id = :playlistId
        ORDER BY position, id
        """,
    )
    abstract suspend fun entries(ownerUserId: String, playlistId: String): List<PlaylistEntryEntity>

    @Query(
        "SELECT COUNT(*) FROM playlist_entries WHERE owner_user_id = :ownerUserId AND playlist_id = :playlistId",
    )
    abstract suspend fun entryCount(ownerUserId: String, playlistId: String): Int

    @Transaction
    open suspend fun snapshot(ownerUserId: String, playlistId: String): PlaylistSnapshot? {
        val playlist = playlist(ownerUserId, playlistId) ?: return null
        return PlaylistSnapshot(playlist, entries(ownerUserId, playlistId))
    }

    @Query("DELETE FROM playlists WHERE owner_user_id = :ownerUserId AND id = :playlistId")
    abstract suspend fun deletePlaylist(ownerUserId: String, playlistId: String): Int

    @Query("DELETE FROM playlists WHERE owner_user_id = :ownerUserId AND id IN (:playlistIds)")
    abstract suspend fun deletePlaylists(ownerUserId: String, playlistIds: List<String>): Int

    private fun validateEntries(ownerUserId: String, playlistId: String, entries: List<PlaylistEntryEntity>) {
        val positions = HashSet<Int>(entries.size)
        val entryIds = HashSet<String>(entries.size)
        entries.forEach { entry ->
            require(entry.ownerUserId == ownerUserId && entry.playlistId == playlistId) {
                "Playlist entry belongs to a different owner or playlist"
            }
            require(positions.add(entry.position)) { "Playlist entry positions must be unique" }
            require(entryIds.add(entry.id)) { "Playlist entry IDs must be unique" }
        }
    }
}
