package com.xymusic.app.feature.playlist.domain.model

import com.xymusic.app.core.model.media.Artwork
import com.xymusic.app.core.model.media.Track
import java.util.UUID

enum class PlaylistVisibility {
    PRIVATE,
    UNLISTED,
    PUBLIC,
}

enum class PlaylistSort {
    UPDATED_DESC,
    NAME_ASC,
    NAME_DESC,
}

data class PlaylistSummary(
    val id: String,
    val ownerUserId: String,
    val name: String,
    val description: String?,
    val visibility: PlaylistVisibility,
    val cover: Artwork?,
    val trackCount: Int,
    val version: Long,
    val createdAtEpochMillis: Long,
    val updatedAtEpochMillis: Long,
)

data class PlaylistEntry(
    val id: String,
    val position: Int,
    val track: Track,
    val addedByUserId: String,
    val addedAtEpochMillis: Long,
)

data class PlaylistDetail(val playlist: PlaylistSummary, val entries: List<PlaylistEntry>)

data class PlaylistDetailPage(
    val playlist: PlaylistSummary,
    val entries: List<PlaylistEntry>,
    val nextCursor: String?,
)

sealed interface ValueChange<out T> {
    data object Unchanged : ValueChange<Nothing>

    data class Set<T>(val value: T) : ValueChange<T>
}

data class CreatePlaylistCommand(val name: String, val description: String?, val visibility: PlaylistVisibility) {
    init {
        validateName(name)
        validateDescription(description)
    }
}

data class UpdatePlaylistCommand(
    val playlistId: String,
    val expectedVersion: Long,
    val name: ValueChange<String> = ValueChange.Unchanged,
    val description: ValueChange<String?> = ValueChange.Unchanged,
    val visibility: ValueChange<PlaylistVisibility> = ValueChange.Unchanged,
) {
    init {
        requireUuid(playlistId, "playlistId")
        require(expectedVersion >= 1) { "expectedVersion must be positive" }
        require(
            name !is ValueChange.Unchanged ||
                description !is ValueChange.Unchanged ||
                visibility !is ValueChange.Unchanged,
        ) { "At least one playlist field must change" }
        if (name is ValueChange.Set) validateName(name.value)
        if (description is ValueChange.Set) validateDescription(description.value)
    }
}

data class AddPlaylistTrackCommand(
    val playlistId: String,
    val expectedVersion: Long,
    val trackId: String,
    val insertAfterEntryId: String? = null,
) {
    init {
        requireUuid(playlistId, "playlistId")
        requireUuid(trackId, "trackId")
        insertAfterEntryId?.let { requireUuid(it, "insertAfterEntryId") }
        require(expectedVersion >= 1) { "expectedVersion must be positive" }
    }
}

data class RemovePlaylistTrackCommand(val playlistId: String, val entryId: String, val expectedVersion: Long) {
    init {
        requireUuid(playlistId, "playlistId")
        requireUuid(entryId, "entryId")
        require(expectedVersion >= 1) { "expectedVersion must be positive" }
    }
}

data class ReorderPlaylistCommand(
    val playlistId: String,
    val expectedVersion: Long,
    val orderedEntryIds: List<String>,
) {
    init {
        requireUuid(playlistId, "playlistId")
        require(expectedVersion >= 1) { "expectedVersion must be positive" }
        require(orderedEntryIds.distinct().size == orderedEntryIds.size) {
            "orderedEntryIds must be unique"
        }
        orderedEntryIds.forEach { requireUuid(it, "entryId") }
    }
}

data class PlaylistVersionConflict(
    val playlistId: String,
    val expectedVersion: Long,
    val currentVersion: Long?,
    val conflictFields: Set<String>,
)

private fun validateName(value: String) {
    require(value.isNotBlank()) { "Playlist name cannot be blank" }
    require(value.length <= 100) { "Playlist name cannot exceed 100 characters" }
}

private fun validateDescription(value: String?) {
    require(value == null || value.length <= 1_000) {
        "Playlist description cannot exceed 1000 characters"
    }
}

private fun requireUuid(value: String, name: String) {
    require(runCatching { UUID.fromString(value) }.isSuccess) { "$name must be a UUID" }
}
