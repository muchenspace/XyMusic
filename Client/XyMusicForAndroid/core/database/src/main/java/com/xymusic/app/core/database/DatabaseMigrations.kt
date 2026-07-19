package com.xymusic.app.core.database

import androidx.room.migration.Migration
import androidx.sqlite.db.SupportSQLiteDatabase

object DatabaseMigrations {
    val MIGRATION_1_2 =
        object : Migration(1, 2) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL("ALTER TABLE playback_queue ADD COLUMN title TEXT NOT NULL DEFAULT ''")
                db.execSQL(
                    "ALTER TABLE playback_queue ADD COLUMN artist_names_json TEXT NOT NULL DEFAULT '[]'",
                )
                db.execSQL("ALTER TABLE playback_queue ADD COLUMN album_title TEXT")
                db.execSQL("ALTER TABLE playback_queue ADD COLUMN artwork_url TEXT")
                db.execSQL("ALTER TABLE playback_queue ADD COLUMN artwork_cache_key TEXT")
                db.execSQL(
                    "ALTER TABLE playback_queue ADD COLUMN duration_ms INTEGER NOT NULL DEFAULT 0",
                )
            }
        }

    val MIGRATION_2_3 =
        object : Migration(2, 3) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL(
                    """
                    UPDATE playback_queue
                    SET title = COALESCE(
                            NULLIF(title, ''),
                            (SELECT tracks.title FROM tracks WHERE tracks.id = playback_queue.track_id),
                            track_id
                        ),
                        duration_ms = CASE
                            WHEN duration_ms > 0 THEN duration_ms
                            ELSE COALESCE(
                                (
                                    SELECT tracks.duration_ms
                                    FROM tracks
                                    WHERE tracks.id = playback_queue.track_id
                                ),
                                0
                            )
                        END,
                        album_title = COALESCE(
                            album_title,
                            (
                                SELECT albums.title
                                FROM tracks
                                LEFT JOIN albums ON albums.id = tracks.album_id
                                WHERE tracks.id = playback_queue.track_id
                            )
                        ),
                        artwork_url = COALESCE(
                            artwork_url,
                            (
                                SELECT COALESCE(tracks.artwork_url, albums.cover_url)
                                FROM tracks
                                LEFT JOIN albums ON albums.id = tracks.album_id
                                WHERE tracks.id = playback_queue.track_id
                            )
                        ),
                        artwork_cache_key = COALESCE(
                            artwork_cache_key,
                            (
                                SELECT COALESCE(tracks.artwork_cache_key, albums.cover_cache_key)
                                FROM tracks
                                LEFT JOIN albums ON albums.id = tracks.album_id
                                WHERE tracks.id = playback_queue.track_id
                            )
                        )
                    """.trimIndent(),
                )
            }
        }

    val MIGRATION_3_4 =
        object : Migration(3, 4) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL(
                    """
                    CREATE TABLE IF NOT EXISTS offline_tracks (
                        track_id TEXT NOT NULL,
                        title TEXT NOT NULL,
                        artist_names_json TEXT NOT NULL,
                        album_title TEXT,
                        artwork_url TEXT,
                        artwork_cache_key TEXT,
                        duration_ms INTEGER NOT NULL,
                        cache_key TEXT NOT NULL,
                        content_length INTEGER NOT NULL,
                        downloaded_at_epoch_ms INTEGER NOT NULL,
                        PRIMARY KEY(track_id)
                    )
                    """.trimIndent(),
                )
                db.execSQL(
                    "CREATE INDEX IF NOT EXISTS index_offline_tracks_downloaded_at " +
                        "ON offline_tracks(downloaded_at_epoch_ms)",
                )
                db.execSQL(
                    "CREATE UNIQUE INDEX IF NOT EXISTS index_offline_tracks_cache_key " +
                        "ON offline_tracks(cache_key)",
                )
            }
        }

    val MIGRATION_4_5 =
        object : Migration(4, 5) {
            override fun migrate(db: SupportSQLiteDatabase) {
                // Version 4 has no ownership information. Assigning those rows to whichever account
                // happens to sign in next would disclose another account's downloaded media.
                db.execSQL("DROP TABLE offline_tracks")
                db.execSQL(
                    """
                    CREATE TABLE IF NOT EXISTS offline_tracks (
                        owner_user_id TEXT NOT NULL,
                        track_id TEXT NOT NULL,
                        title TEXT NOT NULL,
                        artist_names_json TEXT NOT NULL,
                        album_title TEXT,
                        artwork_url TEXT,
                        artwork_cache_key TEXT,
                        duration_ms INTEGER NOT NULL,
                        cache_key TEXT NOT NULL,
                        content_length INTEGER NOT NULL,
                        downloaded_at_epoch_ms INTEGER NOT NULL,
                        PRIMARY KEY(owner_user_id, track_id)
                    )
                    """.trimIndent(),
                )
                db.execSQL(
                    "CREATE INDEX IF NOT EXISTS index_offline_tracks_owner_downloaded_at " +
                        "ON offline_tracks(owner_user_id, downloaded_at_epoch_ms)",
                )
                db.execSQL(
                    "CREATE INDEX IF NOT EXISTS index_offline_tracks_cache_key " +
                        "ON offline_tracks(cache_key)",
                )
            }
        }

    val MIGRATION_5_6 =
        object : Migration(5, 6) {
            override fun migrate(db: SupportSQLiteDatabase) {
                val playlistOperationTypes =
                    "'CREATE_PLAYLIST', 'UPDATE_PLAYLIST', 'DELETE_PLAYLIST', " +
                        "'ADD_PLAYLIST_ENTRY', " +
                        "'REMOVE_PLAYLIST_ENTRY', 'REORDER_PLAYLIST_ENTRIES'"
                db.execSQL(
                    """
                    DELETE FROM playlist_entries
                    WHERE EXISTS (
                        SELECT 1
                        FROM pending_sync_operations AS pending
                        WHERE pending.owner_user_id = playlist_entries.owner_user_id
                          AND pending.target_id = playlist_entries.playlist_id
                          AND pending.operation_type IN ($playlistOperationTypes)
                    )
                    """.trimIndent(),
                )
                db.execSQL(
                    """
                    DELETE FROM playlists
                    WHERE EXISTS (
                        SELECT 1
                        FROM pending_sync_operations AS pending
                        WHERE pending.owner_user_id = playlists.owner_user_id
                          AND pending.target_id = playlists.id
                          AND pending.operation_type IN ($playlistOperationTypes)
                    )
                    """.trimIndent(),
                )
                db.execSQL(
                    "DELETE FROM pending_sync_operations " +
                        "WHERE operation_type IN ($playlistOperationTypes)",
                )
            }
        }

    val ALL =
        arrayOf(
            MIGRATION_1_2,
            MIGRATION_2_3,
            MIGRATION_3_4,
            MIGRATION_4_5,
            MIGRATION_5_6,
        )
}
