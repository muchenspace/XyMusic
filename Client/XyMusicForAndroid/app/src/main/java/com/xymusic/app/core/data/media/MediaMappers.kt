package com.xymusic.app.core.data.media

import com.xymusic.app.core.data.media.remote.AlbumDetailDto
import com.xymusic.app.core.data.media.remote.AlbumReferenceDto
import com.xymusic.app.core.data.media.remote.AlbumSummaryDto
import com.xymusic.app.core.data.media.remote.ArtistDetailDto
import com.xymusic.app.core.data.media.remote.ArtistReferenceDto
import com.xymusic.app.core.data.media.remote.ArtistSummaryDto
import com.xymusic.app.core.data.media.remote.ArtworkDto
import com.xymusic.app.core.data.media.remote.LyricsResourceDto
import com.xymusic.app.core.data.media.remote.TrackDetailDto
import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import com.xymusic.app.core.database.entity.AlbumArtistCreditEntity
import com.xymusic.app.core.database.entity.AlbumEntity
import com.xymusic.app.core.database.entity.ArtistEntity
import com.xymusic.app.core.database.entity.ArtworkColumns
import com.xymusic.app.core.database.entity.LyricsEntity
import com.xymusic.app.core.database.entity.TrackArtistCreditEntity
import com.xymusic.app.core.database.entity.TrackEntity
import com.xymusic.app.core.database.model.AlbumReadModel
import com.xymusic.app.core.database.model.ArtistCreditRole
import com.xymusic.app.core.database.model.LyricsFormat as DatabaseLyricsFormat
import com.xymusic.app.core.database.model.TrackDetailReadModel
import com.xymusic.app.core.database.model.TrackSummaryReadModel
import com.xymusic.app.core.model.media.Album
import com.xymusic.app.core.model.media.AlbumReference
import com.xymusic.app.core.model.media.Artist
import com.xymusic.app.core.model.media.ArtistReference
import com.xymusic.app.core.model.media.Artwork
import com.xymusic.app.core.model.media.Lyrics
import com.xymusic.app.core.model.media.LyricsFormat
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.core.model.media.TrackDetail
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset
import java.util.UUID

internal data class AlbumWriteModel(
    val album: AlbumEntity,
    val artistReferences: List<ArtistEntity>,
    val credits: List<AlbumArtistCreditEntity>,
)

internal data class TrackWriteModel(
    val track: TrackEntity,
    val artistReferences: List<ArtistEntity>,
    val albumReference: AlbumEntity?,
    val credits: List<TrackArtistCreditEntity>,
    val lyrics: List<LyricsEntity>?,
)

internal fun ArtistReferenceDto.toReferenceEntity(cachedAtEpochMs: Long): ArtistEntity {
    requireUuid(id, "artist ID")
    require(name.isNotBlank()) { "Artist name cannot be blank" }
    return ArtistEntity(id, name, null, null, cachedAtEpochMs)
}

internal fun ArtistSummaryDto.toEntity(cachedAtEpochMs: Long): ArtistEntity {
    requireUuid(id, "artist ID")
    require(name.isNotBlank()) { "Artist name cannot be blank" }
    return ArtistEntity(id, name, null, artwork.toColumns(), cachedAtEpochMs)
}

internal fun ArtistDetailDto.toEntity(cachedAtEpochMs: Long): ArtistEntity {
    requireUuid(id, "artist ID")
    require(name.isNotBlank()) { "Artist name cannot be blank" }
    return ArtistEntity(id, name, description, artwork.toColumns(), cachedAtEpochMs)
}

internal fun AlbumReferenceDto.toReferenceEntity(cachedAtEpochMs: Long): AlbumEntity {
    requireUuid(id, "album ID")
    require(title.isNotBlank()) { "Album title cannot be blank" }
    return AlbumEntity(id, title, null, null, 0, null, cachedAtEpochMs)
}

internal fun AlbumSummaryDto.toWriteModel(cachedAtEpochMs: Long): AlbumWriteModel = albumWriteModel(
    id = id,
    title = title,
    artists = artists,
    cover = cover,
    releaseDate = releaseDate,
    trackCount = trackCount,
    description = null,
    cachedAtEpochMs = cachedAtEpochMs,
)

internal fun AlbumDetailDto.toWriteModel(cachedAtEpochMs: Long): AlbumWriteModel = albumWriteModel(
    id = id,
    title = title,
    artists = artists,
    cover = cover,
    releaseDate = releaseDate,
    trackCount = trackCount,
    description = description,
    cachedAtEpochMs = cachedAtEpochMs,
)

private fun albumWriteModel(
    id: String,
    title: String,
    artists: List<ArtistReferenceDto>,
    cover: ArtworkDto?,
    releaseDate: String?,
    trackCount: Int,
    description: String?,
    cachedAtEpochMs: Long,
): AlbumWriteModel {
    requireUuid(id, "album ID")
    require(title.isNotBlank()) { "Album title cannot be blank" }
    require(trackCount >= 0) { "Album track count cannot be negative" }
    requireUniqueReferences(artists, "album artist")
    val references = artists.map { it.toReferenceEntity(cachedAtEpochMs) }
    return AlbumWriteModel(
        album =
        AlbumEntity(
            id = id,
            title = title,
            description = description,
            releaseDateEpochMs = releaseDate?.let(::dateToEpochMillis),
            trackCount = trackCount,
            cover = cover.toColumns(),
            cachedAtEpochMs = cachedAtEpochMs,
        ),
        artistReferences = references,
        credits =
        references.mapIndexed { index, artist ->
            AlbumArtistCreditEntity(id, artist.id, ArtistCreditRole.PRIMARY, index)
        },
    )
}

internal fun TrackSummaryDto.toWriteModel(cachedAtEpochMs: Long): TrackWriteModel = trackWriteModel(
    id = id,
    title = title,
    artists = artists,
    album = album,
    artwork = artwork,
    durationMs = durationMs,
    trackNumber = trackNumber,
    discNumber = discNumber,
    publishedAt = publishedAt,
    lyrics = null,
    cachedAtEpochMs = cachedAtEpochMs,
)

internal fun TrackDetailDto.toWriteModel(cachedAtEpochMs: Long): TrackWriteModel = trackWriteModel(
    id = id,
    title = title,
    artists = artists,
    album = album,
    artwork = artwork,
    durationMs = durationMs,
    trackNumber = trackNumber,
    discNumber = discNumber,
    publishedAt = publishedAt,
    lyrics = lyrics,
    cachedAtEpochMs = cachedAtEpochMs,
)

private fun trackWriteModel(
    id: String,
    title: String,
    artists: List<ArtistReferenceDto>,
    album: AlbumReferenceDto?,
    artwork: ArtworkDto?,
    durationMs: Long,
    trackNumber: Int?,
    discNumber: Int,
    publishedAt: String,
    lyrics: List<LyricsResourceDto>?,
    cachedAtEpochMs: Long,
): TrackWriteModel {
    requireUuid(id, "track ID")
    require(title.isNotBlank()) { "Track title cannot be blank" }
    require(durationMs > 0) { "Track duration must be positive" }
    require(trackNumber == null || trackNumber > 0) { "Track number must be positive" }
    require(discNumber > 0) { "Disc number must be positive" }
    requireUniqueReferences(artists, "track artist")
    val references = artists.map { it.toReferenceEntity(cachedAtEpochMs) }
    val lyricEntities =
        lyrics
            ?.also { resources ->
                requireUniqueIds(resources.map(LyricsResourceDto::id), "lyrics")
                require(resources.all { it.trackId == id }) { "Lyrics belong to a different track" }
            }?.map(LyricsResourceDto::toEntity)
    return TrackWriteModel(
        track =
        TrackEntity(
            id = id,
            albumId = album?.id,
            title = title,
            durationMs = durationMs,
            trackNumber = trackNumber,
            discNumber = discNumber,
            publishedAtEpochMs = Instant.parse(publishedAt).toEpochMilli(),
            artwork = artwork.toColumns(),
            cachedAtEpochMs = cachedAtEpochMs,
        ),
        artistReferences = references,
        albumReference = album?.toReferenceEntity(cachedAtEpochMs),
        credits =
        references.mapIndexed { index, artist ->
            TrackArtistCreditEntity(id, artist.id, ArtistCreditRole.PRIMARY, index)
        },
        lyrics = lyricEntities,
    )
}

private fun LyricsResourceDto.toEntity(): LyricsEntity {
    requireUuid(id, "lyrics ID")
    requireUuid(trackId, "lyrics track ID")
    require(trackVersion >= 1) { "Lyrics track version must be positive" }
    return LyricsEntity(
        id = id,
        trackId = trackId,
        language = language,
        format = DatabaseLyricsFormat.valueOf(format),
        content = content,
        isDefault = isDefault,
        trackVersion = trackVersion,
        updatedAtEpochMs = Instant.parse(updatedAt).toEpochMilli(),
    )
}

internal fun TrackSummaryReadModel.toDomain(): Track = track.toDomain(album, credits, artists)

internal fun TrackDetailReadModel.toDomain(): TrackDetail = TrackDetail(
    track = track.toDomain(album, credits, artists),
    lyrics =
    lyrics
        .sortedWith(
            compareByDescending<LyricsEntity> { it.isDefault }
                .thenBy(LyricsEntity::language)
                .thenBy { it.format.name },
        ).map { lyric ->
            Lyrics(
                id = lyric.id,
                trackId = lyric.trackId,
                language = lyric.language,
                format = LyricsFormat.valueOf(lyric.format.name),
                content = lyric.content,
                isDefault = lyric.isDefault,
                trackVersion = lyric.trackVersion,
                updatedAtEpochMillis = lyric.updatedAtEpochMs,
            )
        },
)

private fun TrackEntity.toDomain(
    album: AlbumEntity?,
    credits: List<TrackArtistCreditEntity>,
    artists: List<ArtistEntity>,
): Track {
    val artistsById = artists.associateBy(ArtistEntity::id)
    return Track(
        id = id,
        title = title,
        artists =
        credits
            .sortedBy(TrackArtistCreditEntity::sortOrder)
            .mapNotNull { credit ->
                artistsById[credit.artistId]?.let { ArtistReference(it.id, it.name) }
            }.distinctBy(ArtistReference::id),
        album = album?.let { AlbumReference(it.id, it.title) },
        artwork = artwork.toDomain(),
        durationMs = durationMs,
        trackNumber = trackNumber,
        discNumber = discNumber,
        publishedAtEpochMillis = publishedAtEpochMs,
    )
}

internal fun ArtistEntity.toDomain(): Artist = Artist(
    id = id,
    name = name,
    artwork = artwork.toDomain(),
    description = description,
)

internal fun AlbumReadModel.toDomain(): Album {
    val artistsById = artists.associateBy(ArtistEntity::id)
    return Album(
        id = album.id,
        title = album.title,
        artists =
        credits
            .sortedBy(AlbumArtistCreditEntity::sortOrder)
            .mapNotNull { credit ->
                artistsById[credit.artistId]?.let { ArtistReference(it.id, it.name) }
            }.distinctBy(ArtistReference::id),
        cover = album.cover.toDomain(),
        releaseDateEpochMillis = album.releaseDateEpochMs,
        trackCount = album.trackCount,
        description = album.description,
    )
}

private fun ArtworkDto?.toColumns(): ArtworkColumns? = this?.let { artwork ->
    requireUuid(artwork.assetId, "artwork asset ID")
    require(artwork.url.isNotBlank()) { "Artwork URL cannot be blank" }
    require(artwork.cacheKey.isNotBlank()) { "Artwork cache key cannot be blank" }
    require(artwork.mimeType.isNotBlank()) { "Artwork MIME type cannot be blank" }
    ArtworkColumns(
        assetId = artwork.assetId,
        url = artwork.url,
        cacheKey = artwork.cacheKey,
        mimeType = artwork.mimeType,
        expiresAtEpochMs = artwork.expiresAt?.let { Instant.parse(it).toEpochMilli() },
        width = artwork.width,
        height = artwork.height,
    )
}

private fun ArtworkColumns?.toDomain(): Artwork? = this?.assetId?.let { assetId ->
    Artwork(
        assetId = assetId,
        url = requireNotNull(url),
        cacheKey = requireNotNull(cacheKey),
        mimeType = requireNotNull(mimeType),
        expiresAtEpochMillis = expiresAtEpochMs,
        width = width,
        height = height,
    )
}

private fun requireUniqueReferences(artists: List<ArtistReferenceDto>, label: String) {
    require(artists.isNotEmpty()) { "$label list cannot be empty" }
    requireUniqueIds(artists.map(ArtistReferenceDto::id), label)
}

internal fun requireUniqueIds(ids: List<String>, label: String) {
    require(ids.distinct().size == ids.size) { "Duplicate $label ID" }
}

private fun requireUuid(value: String, label: String) {
    require(runCatching { UUID.fromString(value) }.isSuccess) { "Invalid $label" }
}

private fun dateToEpochMillis(value: String): Long = LocalDate
    .parse(value)
    .atStartOfDay()
    .toInstant(ZoneOffset.UTC)
    .toEpochMilli()
