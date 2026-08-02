package com.xymusic.app.core.database

import android.app.Application
import androidx.room.testing.MigrationTestHelper
import androidx.sqlite.driver.AndroidSQLiteDriver
import androidx.sqlite.execSQL
import androidx.test.platform.app.InstrumentationRegistry
import com.google.common.truth.Truth.assertThat
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class LyricsTimingDatabaseMigrationTest {
    @get:Rule
    val helper =
        MigrationTestHelper(
            instrumentation = InstrumentationRegistry.getInstrumentation(),
            file = InstrumentationRegistry.getInstrumentation().targetContext.getDatabasePath(TEST_DATABASE),
            driver = AndroidSQLiteDriver(),
            databaseClass = XyMusicDatabase::class,
        )

    @Test
    fun migrationSixToSevenClearsLyricsWithoutAuthoritativeServerTiming() {
        helper.createDatabase(6).apply {
            execSQL(
                """
                INSERT INTO tracks(
                    id, album_id, title, duration_ms, track_number, disc_number,
                    published_at_epoch_ms, cached_at_epoch_ms
                ) VALUES ('track-timing', NULL, 'Track', 1000, NULL, 1, 1, 1)
                """.trimIndent(),
            )
            execSQL(
                """
                INSERT INTO lyrics(
                    id, track_id, language, format, content, is_default,
                    track_version, updated_at_epoch_ms
                ) VALUES
                    ('lyrics-line', 'track-timing', 'zh', 'LRC', '[00:00]Line', 1, 1, 1),
                    ('lyrics-word', 'track-timing', 'en', 'LRC',
                     '[00:00]<00:00.000>Word', 0, 1, 1)
                """.trimIndent(),
            )
            close()
        }

        helper
            .runMigrationsAndValidate(
                XyMusicDatabase.VERSION,
                listOf(DatabaseMigrations.MIGRATION_6_7),
            ).use { database ->
                database.prepare("SELECT COUNT(*) FROM lyrics").use { statement ->
                    assertThat(statement.step()).isTrue()
                    assertThat(statement.getLong(0)).isEqualTo(0)
                }
            }
    }

    private companion object {
        const val TEST_DATABASE = "lyrics-timing-migration-test"
    }
}
