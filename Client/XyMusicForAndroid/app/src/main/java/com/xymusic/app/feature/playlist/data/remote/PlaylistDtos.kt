package com.xymusic.app.feature.playlist.data.remote

import com.xymusic.app.core.data.media.remote.ArtworkDto
import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import kotlinx.serialization.Serializable

@Serializable
data class UserSummaryDto(val id: String, val username: String, val displayName: String, val avatar: ArtworkDto?)

@Serializable
data class PlaylistSummaryDto(
    val id: String,
    val owner: UserSummaryDto,
    val name: String,
    val description: String?,
    val visibility: String,
    val cover: ArtworkDto?,
    val trackCount: Int,
    val version: Long,
    val createdAt: String,
    val updatedAt: String,
)

@Serializable
data class PlaylistEntryDto(
    val id: String,
    val position: Int,
    val track: TrackSummaryDto,
    val addedBy: UserSummaryDto,
    val addedAt: String,
)

@Serializable
data class PlaylistDetailDto(
    val id: String,
    val owner: UserSummaryDto,
    val name: String,
    val description: String?,
    val visibility: String,
    val cover: ArtworkDto?,
    val trackCount: Int,
    val version: Long,
    val createdAt: String,
    val updatedAt: String,
    val entries: List<PlaylistEntryDto>,
    val nextCursor: String?,
)

@Serializable
data class PlaylistPageDto(val items: List<PlaylistSummaryDto>, val nextCursor: String?)

@Serializable
data class CreatePlaylistRequestDto(val name: String, val description: String?, val visibility: String)

@Serializable
data class PlaylistUpdatePayload(
    val expectedVersion: Long,
    val namePresent: Boolean,
    val name: String?,
    val descriptionPresent: Boolean,
    val description: String?,
    val visibilityPresent: Boolean,
    val visibility: String?,
)

@Serializable
data class AddPlaylistTrackRequestDto(val expectedVersion: Long, val trackId: String, val insertAfterEntryId: String?)

@Serializable
data class ReorderPlaylistRequestDto(val expectedVersion: Long, val orderedEntryIds: List<String>)

@Serializable
data class PlaylistMutationDto(val playlistId: String, val version: Long, val updatedAt: String)

@Serializable
data class PlaylistEntryMutationDto(
    val playlistId: String,
    val version: Long,
    val updatedAt: String,
    val entry: PlaylistEntryDto,
)
