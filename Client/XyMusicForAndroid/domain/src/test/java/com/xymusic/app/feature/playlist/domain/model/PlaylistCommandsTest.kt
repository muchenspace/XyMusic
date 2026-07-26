package com.xymusic.app.feature.playlist.domain.model

import com.google.common.truth.Truth.assertThat
import java.util.UUID
import org.junit.Assert.assertThrows
import org.junit.Test

class PlaylistCommandsTest {
    private val playlistId = UUID.randomUUID().toString()
    private val entryId = UUID.randomUUID().toString()

    @Test
    fun createPlaylistValidatesNameAndDescription() {
        val command = CreatePlaylistCommand("我的歌单", null, PlaylistVisibility.PRIVATE)

        assertThat(command.name).isEqualTo("我的歌单")
        assertThrows(IllegalArgumentException::class.java) {
            CreatePlaylistCommand("  ", null, PlaylistVisibility.PRIVATE)
        }
        assertThrows(IllegalArgumentException::class.java) {
            CreatePlaylistCommand("a".repeat(101), null, PlaylistVisibility.PRIVATE)
        }
        assertThrows(IllegalArgumentException::class.java) {
            CreatePlaylistCommand("ok", "b".repeat(1_001), PlaylistVisibility.PRIVATE)
        }
    }

    @Test
    fun updatePlaylistRequiresAtLeastOneChange() {
        assertThrows(IllegalArgumentException::class.java) {
            UpdatePlaylistCommand(playlistId = playlistId, expectedVersion = 1)
        }
    }

    @Test
    fun updatePlaylistRejectsInvalidIdAndVersion() {
        assertThrows(IllegalArgumentException::class.java) {
            UpdatePlaylistCommand(
                playlistId = "not-a-uuid",
                expectedVersion = 1,
                name = ValueChange.Set("新名字"),
            )
        }
        assertThrows(IllegalArgumentException::class.java) {
            UpdatePlaylistCommand(
                playlistId = playlistId,
                expectedVersion = 0,
                name = ValueChange.Set("新名字"),
            )
        }
    }

    @Test
    fun reorderRejectsDuplicateEntries() {
        assertThrows(IllegalArgumentException::class.java) {
            ReorderPlaylistCommand(
                playlistId = playlistId,
                expectedVersion = 1,
                orderedEntryIds = listOf(entryId, entryId),
            )
        }
    }

    @Test
    fun removeTrackValidatesIdentifiers() {
        val command = RemovePlaylistTrackCommand(playlistId, entryId, expectedVersion = 2)

        assertThat(command.expectedVersion).isEqualTo(2)
        assertThrows(IllegalArgumentException::class.java) {
            RemovePlaylistTrackCommand(playlistId, "bad", expectedVersion = 2)
        }
    }
}
