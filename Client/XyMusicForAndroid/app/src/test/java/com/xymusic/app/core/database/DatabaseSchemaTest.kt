package com.xymusic.app.core.database

import com.google.common.truth.Truth.assertThat
import java.io.File
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Test

class DatabaseSchemaTest {
    @Test
    fun exportedCurrentSchemaContainsEveryTableAndOwnerBoundary() {
        val schemaFile =
            File(
                "schemas/com.xymusic.app.core.database.XyMusicDatabase/${XyMusicDatabase.VERSION}.json",
            )
        assertThat(schemaFile.isFile).isTrue()
        val database =
            Json
                .parseToJsonElement(schemaFile.readText())
                .jsonObject
                .getValue("database")
                .jsonObject
        val entities =
            database.getValue("entities").jsonArray.associateBy {
                it.jsonObject
                    .getValue("tableName")
                    .jsonPrimitive.content
            }

        assertThat(entities.keys).containsAtLeast(
            "artists",
            "albums",
            "album_artist_credits",
            "tracks",
            "track_artist_credits",
            "lyrics",
            "favorites",
            "playback_history",
            "playlists",
            "playlist_entries",
            "playback_queue",
            "search_history",
            "catalog_remote_keys",
            "pending_sync_operations",
            "offline_tracks",
        )

        val privateTables =
            listOf(
                "favorites",
                "playback_history",
                "playlists",
                "playlist_entries",
                "playback_queue",
                "search_history",
                "pending_sync_operations",
                "offline_tracks",
            )
        privateTables.forEach { table ->
            val columns =
                entities.getValue(table).jsonObject.getValue("fields").jsonArray.map {
                    it.jsonObject
                        .getValue("columnName")
                        .jsonPrimitive.content
                }
            assertThat(columns).contains("owner_user_id")
        }
    }

    @Test
    fun schemaDoesNotPersistCredentialsOrTemporaryPlaybackUrls() {
        val schema =
            File(
                "schemas/com.xymusic.app.core.database.XyMusicDatabase/${XyMusicDatabase.VERSION}.json",
            ).readText().lowercase()

        assertThat(schema).doesNotContain("access_token")
        assertThat(schema).doesNotContain("refresh_token")
        assertThat(schema).doesNotContain("signed_url")
        assertThat(schema).doesNotContain("playback_url")
        assertThat(schema).doesNotContain("grant_url")
    }
}
