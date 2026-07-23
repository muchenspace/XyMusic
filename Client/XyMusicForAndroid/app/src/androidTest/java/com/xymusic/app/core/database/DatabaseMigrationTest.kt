package com.xymusic.app.core.database

import androidx.room.testing.MigrationTestHelper
import androidx.sqlite.db.SupportSQLiteDatabase
import androidx.sqlite.db.framework.FrameworkSQLiteOpenHelperFactory
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import com.google.common.truth.Truth.assertThat
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class DatabaseMigrationTest {
    @get:Rule
    val helper =
        MigrationTestHelper(
            InstrumentationRegistry.getInstrumentation(),
            XyMusicDatabase::class.java,
            emptyList(),
            FrameworkSQLiteOpenHelperFactory(),
        )

    @Test
    fun migrationOneToThreeAddsAndRepairsDurableQueueMetadata() {
        helper.createDatabase(TEST_DATABASE, 1).close()
        helper
            .runMigrationsAndValidate(
                TEST_DATABASE,
                XyMusicDatabase.VERSION,
                true,
                DatabaseMigrations.MIGRATION_1_2,
                DatabaseMigrations.MIGRATION_2_3,
                DatabaseMigrations.MIGRATION_3_4,
                DatabaseMigrations.MIGRATION_4_5,
                DatabaseMigrations.MIGRATION_5_6,
            ).close()
    }

    @Test
    fun migrationTwoToThreeBackfillsQueueMetadataFromCatalog() {
        helper.createDatabase(TEST_DATABASE, 2).apply {
            execSQL(
                """
                INSERT INTO albums(
                    id, title, track_count, cached_at_epoch_ms, cover_url, cover_cache_key
                ) VALUES ('album', 'Album title', 1, 1, 'https://cover', 'album-cover')
                """.trimIndent(),
            )
            execSQL(
                """
                INSERT INTO tracks(
                    id, album_id, title, duration_ms, disc_number,
                    published_at_epoch_ms, cached_at_epoch_ms
                ) VALUES (
                    '00000000-0000-0000-0000-000000000001',
                    'album',
                    'Track title',
                    123456,
                    1,
                    1,
                    1
                )
                """.trimIndent(),
            )
            execSQL(
                """
                INSERT INTO playback_queue(
                    owner_user_id, item_id, position, track_id,
                    resume_position_ms, is_current, enqueued_at_epoch_ms
                ) VALUES (
                    'owner',
                    'queue-item',
                    0,
                    '00000000-0000-0000-0000-000000000001',
                    321,
                    1,
                    1
                )
                """.trimIndent(),
            )
            close()
        }

        helper
            .runMigrationsAndValidate(
                TEST_DATABASE,
                XyMusicDatabase.VERSION,
                true,
                DatabaseMigrations.MIGRATION_2_3,
                DatabaseMigrations.MIGRATION_3_4,
                DatabaseMigrations.MIGRATION_4_5,
                DatabaseMigrations.MIGRATION_5_6,
            ).use { database ->
                database
                    .query(
                        """
                        SELECT title, duration_ms, album_title, artwork_url, artwork_cache_key
                        FROM playback_queue
                        WHERE owner_user_id = 'owner' AND item_id = 'queue-item'
                        """.trimIndent(),
                    ).use { cursor ->
                        assertThat(cursor.moveToFirst()).isTrue()
                        assertThat(cursor.getString(0)).isEqualTo("Track title")
                        assertThat(cursor.getLong(1)).isEqualTo(123456L)
                        assertThat(cursor.getString(2)).isEqualTo("Album title")
                        assertThat(cursor.getString(3)).isEqualTo("https://cover")
                        assertThat(cursor.getString(4)).isEqualTo("album-cover")
                    }
            }
    }

    @Test
    fun migrationFourToFiveDiscardsLegacyUnownedOfflineMetadata() {
        helper.createDatabase(TEST_DATABASE, 4).apply {
            execSQL(
                """
                INSERT INTO offline_tracks(
                    track_id, title, artist_names_json, duration_ms, cache_key,
                    content_length, downloaded_at_epoch_ms
                ) VALUES (
                    'legacy-track', 'Legacy', '[]', 1000, 'legacy-cache', 128, 1
                )
                """.trimIndent(),
            )
            close()
        }

        helper
            .runMigrationsAndValidate(
                TEST_DATABASE,
                XyMusicDatabase.VERSION,
                true,
                DatabaseMigrations.MIGRATION_4_5,
                DatabaseMigrations.MIGRATION_5_6,
            ).use { database ->
                database.query("SELECT COUNT(*) FROM offline_tracks").use { cursor ->
                    assertThat(cursor.moveToFirst()).isTrue()
                    assertThat(cursor.getInt(0)).isEqualTo(0)
                }
            }
    }

    @Test
    fun migrationFiveToSixDiscardsPlaylistPendingStateAndPreservesLibraryPending() {
        helper.createDatabase(TEST_DATABASE, 5).apply {
            execSQL(
                """
                INSERT INTO tracks(
                    id, title, duration_ms, disc_number,
                    published_at_epoch_ms, cached_at_epoch_ms
                ) VALUES ('track', 'Track', 1000, 1, 1, 1)
                """.trimIndent(),
            )
            execSQL(
                """
                INSERT INTO playlists(
                    owner_user_id, id, name, visibility, track_count, version,
                    created_at_epoch_ms, updated_at_epoch_ms
                ) VALUES ('owner', 'playlist', 'Optimistic', 'PRIVATE', 1, 2, 1, 1)
                """.trimIndent(),
            )
            execSQL(
                """
                INSERT INTO playlists(
                    owner_user_id, id, name, visibility, track_count, version,
                    created_at_epoch_ms, updated_at_epoch_ms
                ) VALUES ('owner', 'preserved', 'Preserved', 'PRIVATE', 0, 1, 1, 1)
                """.trimIndent(),
            )
            execSQL(
                """
                INSERT INTO playlist_entries(
                    owner_user_id, id, playlist_id, position, track_id,
                    added_by_user_id, added_at_epoch_ms
                ) VALUES ('owner', 'entry', 'playlist', 0, 'track', 'owner', 1)
                """.trimIndent(),
            )
            insertPendingOperation(
                id = "playlist-operation",
                operationType = "ADD_PLAYLIST_ENTRY",
                targetType = "PLAYLIST",
                targetId = "playlist",
                status = "CONFLICT",
            )
            insertPendingOperation(
                id = "favorite-operation",
                operationType = "ADD_FAVORITE",
                targetType = "FAVORITE",
                targetId = "track",
                status = "PENDING",
            )
            close()
        }

        helper
            .runMigrationsAndValidate(
                TEST_DATABASE,
                XyMusicDatabase.VERSION,
                true,
                DatabaseMigrations.MIGRATION_5_6,
            ).use { database ->
                assertCount(database, "playlists", "id = 'playlist'", 0)
                assertCount(database, "playlist_entries", "playlist_id = 'playlist'", 0)
                assertCount(database, "playlists", "id = 'preserved'", 1)
                assertCount(
                    database,
                    "pending_sync_operations",
                    "id = 'playlist-operation'",
                    0,
                )
                assertCount(
                    database,
                    "pending_sync_operations",
                    "id = 'favorite-operation'",
                    1,
                )
            }
    }

    private fun SupportSQLiteDatabase.insertPendingOperation(
        id: String,
        operationType: String,
        targetType: String,
        targetId: String,
        status: String,
    ) {
        execSQL(
            """
            INSERT INTO pending_sync_operations(
                owner_user_id, id, operation_type, target_type, target_id,
                idempotency_key, status, attempt_count, created_at_epoch_ms,
                updated_at_epoch_ms, next_attempt_at_epoch_ms
            ) VALUES (
                'owner', '$id', '$operationType', '$targetType', '$targetId',
                '$id-key', '$status', 0, 1, 1, 1
            )
            """.trimIndent(),
        )
    }

    private fun assertCount(database: SupportSQLiteDatabase, table: String, predicate: String, expected: Int) {
        database.query("SELECT COUNT(*) FROM $table WHERE $predicate").use { cursor ->
            assertThat(cursor.moveToFirst()).isTrue()
            assertThat(cursor.getInt(0)).isEqualTo(expected)
        }
    }

    companion object {
        private const val TEST_DATABASE = "migration-test"
    }
}
