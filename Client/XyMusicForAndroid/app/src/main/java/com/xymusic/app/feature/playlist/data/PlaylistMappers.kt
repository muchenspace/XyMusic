package com.xymusic.app.feature.playlist.data

import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import com.xymusic.app.core.data.media.toArtworkColumns
import com.xymusic.app.core.data.media.toDomain
import com.xymusic.app.core.data.media.toDomainArtwork
import com.xymusic.app.core.database.dao.CatalogDao
import com.xymusic.app.core.database.dao.PlaylistSnapshot
import com.xymusic.app.core.database.entity.PlaylistEntity
import com.xymusic.app.core.database.entity.PlaylistEntryEntity
import com.xymusic.app.core.database.model.toDomainArtwork as toDomainArtworkColumns
import com.xymusic.app.core.model.media.AlbumReference
import com.xymusic.app.core.model.media.ArtistReference
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.feature.playlist.data.remote.PlaylistDetailDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistEntryDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistSummaryDto
import com.xymusic.app.feature.playlist.domain.model.PlaylistDetail
import com.xymusic.app.feature.playlist.domain.model.PlaylistDetailPage
import com.xymusic.app.feature.playlist.domain.model.PlaylistEntry
import com.xymusic.app.feature.playlist.domain.model.PlaylistSummary
import com.xymusic.app.feature.playlist.domain.model.PlaylistVisibility
import java.time.Instant

internal fun PlaylistSummaryDto.toEntity(ownerUserId: String): PlaylistEntity {
    require(owner.id == ownerUserId) { "Playlist owner does not match the current user" }
    return PlaylistEntity(
        ownerUserId = ownerUserId,
        id = id,
        name = name,
        description = description,
        visibility = PlaylistVisibility.fromWireValue(visibility),
        cover = cover.toArtworkColumns(),
        trackCount = trackCount,
        version = version,
        createdAtEpochMs = Instant.parse(createdAt).toEpochMilli(),
        updatedAtEpochMs = Instant.parse(updatedAt).toEpochMilli(),
    ).also { entity ->
        require(entity.trackCount >= 0) { "Playlist track count cannot be negative" }
        require(entity.version >= 1) { "Playlist version must be positive" }
    }
}

internal fun PlaylistDetailDto.toEntity(ownerUserId: String): PlaylistEntity = PlaylistSummaryDto(
    id,
    owner,
    name,
    description,
    visibility,
    cover,
    trackCount,
    version,
    createdAt,
    updatedAt,
).toEntity(ownerUserId)

internal fun PlaylistDetailDto.toDomainPage(ownerUserId: String): PlaylistDetailPage = PlaylistDetailPage(
    playlist = toEntity(ownerUserId).toDomain(),
    entries = entries.sortedByDescending(PlaylistEntryDto::position).map(PlaylistEntryDto::toDomain),
    nextCursor = nextCursor,
)

private fun PlaylistEntryDto.toDomain(): PlaylistEntry = PlaylistEntry(
    id = id,
    position = position,
    track = track.toDomainTrack(),
    addedByUserId = addedBy.id,
    addedAtEpochMillis = Instant.parse(addedAt).toEpochMilli(),
)

private fun TrackSummaryDto.toDomainTrack(): Track = Track(
    id = id,
    title = title,
    artists = artists.map { artist -> ArtistReference(artist.id, artist.name) },
    album = album?.let { item -> AlbumReference(item.id, item.title) },
    artwork = artwork.toDomainArtwork(),
    durationMs = durationMs,
    trackNumber = trackNumber,
    discNumber = discNumber,
    publishedAtEpochMillis = Instant.parse(publishedAt).toEpochMilli(),
)

internal fun PlaylistEntryDto.toEntity(ownerUserId: String, playlistId: String): PlaylistEntryEntity =
    PlaylistEntryEntity(
        ownerUserId = ownerUserId,
        id = id,
        playlistId = playlistId,
        position = position,
        trackId = track.id,
        addedByUserId = addedBy.id,
        addedAtEpochMs = Instant.parse(addedAt).toEpochMilli(),
    )

internal fun PlaylistEntity.toDomain(): PlaylistSummary = PlaylistSummary(
    id = id,
    ownerUserId = ownerUserId,
    name = name,
    description = description,
    visibility = visibility,
    cover = cover.toDomainArtworkColumns(),
    trackCount = trackCount,
    version = version,
    createdAtEpochMillis = createdAtEpochMs,
    updatedAtEpochMillis = updatedAtEpochMs,
)

internal suspend fun PlaylistSnapshot.toDomain(catalogDao: CatalogDao): PlaylistDetail {
    val tracks =
        if (entries.isEmpty()) {
            emptyMap()
        } else {
            catalogDao
                .tracksInBatches(entries.map(PlaylistEntryEntity::trackId))
                .associateBy { model -> model.track.id }
        }
    return PlaylistDetail(
        playlist = playlist.toDomain(),
        entries =
        entries.sortedByDescending(PlaylistEntryEntity::position).mapNotNull { entry ->
            tracks[entry.trackId]?.let { track ->
                PlaylistEntry(
                    id = entry.id,
                    position = entry.position,
                    track = track.toDomain(),
                    addedByUserId = entry.addedByUserId,
                    addedAtEpochMillis = entry.addedAtEpochMs,
                )
            }
        },
    )
}
