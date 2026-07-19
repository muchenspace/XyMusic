package com.xymusic.app.core.database.model

import androidx.room.ColumnInfo
import androidx.room.Embedded
import androidx.room.Junction
import androidx.room.Relation
import com.xymusic.app.core.database.entity.AlbumArtistCreditEntity
import com.xymusic.app.core.database.entity.AlbumEntity
import com.xymusic.app.core.database.entity.ArtistEntity
import com.xymusic.app.core.database.entity.LyricsEntity
import com.xymusic.app.core.database.entity.TrackArtistCreditEntity
import com.xymusic.app.core.database.entity.TrackEntity

data class TrackSummaryReadModel(
    @Embedded val track: TrackEntity,
    @Relation(parentColumn = "album_id", entityColumn = "id")
    val album: AlbumEntity?,
    @Relation(parentColumn = "id", entityColumn = "track_id")
    val credits: List<TrackArtistCreditEntity>,
    @Relation(
        parentColumn = "id",
        entityColumn = "id",
        associateBy =
        Junction(
            value = TrackArtistCreditEntity::class,
            parentColumn = "track_id",
            entityColumn = "artist_id",
        ),
    )
    val artists: List<ArtistEntity>,
)

data class TrackDetailReadModel(
    @Embedded val track: TrackEntity,
    @Relation(parentColumn = "album_id", entityColumn = "id")
    val album: AlbumEntity?,
    @Relation(parentColumn = "id", entityColumn = "track_id")
    val credits: List<TrackArtistCreditEntity>,
    @Relation(
        parentColumn = "id",
        entityColumn = "id",
        associateBy =
        Junction(
            value = TrackArtistCreditEntity::class,
            parentColumn = "track_id",
            entityColumn = "artist_id",
        ),
    )
    val artists: List<ArtistEntity>,
    @Relation(parentColumn = "id", entityColumn = "track_id")
    val lyrics: List<LyricsEntity>,
)

data class AlbumReadModel(
    @Embedded val album: AlbumEntity,
    @Relation(parentColumn = "id", entityColumn = "album_id")
    val credits: List<AlbumArtistCreditEntity>,
    @Relation(
        parentColumn = "id",
        entityColumn = "id",
        associateBy =
        Junction(
            value = AlbumArtistCreditEntity::class,
            parentColumn = "album_id",
            entityColumn = "artist_id",
        ),
    )
    val artists: List<ArtistEntity>,
)

data class PlaybackHistoryReadModel(
    @Embedded val track: TrackEntity,
    @ColumnInfo(name = "history_last_position_ms") val lastPositionMs: Long,
    @ColumnInfo(name = "history_play_count") val playCount: Long,
    @ColumnInfo(name = "history_last_played_at_epoch_ms") val lastPlayedAtEpochMs: Long,
    @ColumnInfo(name = "history_completed") val completed: Boolean,
    @ColumnInfo(name = "history_updated_at_epoch_ms") val updatedAtEpochMs: Long,
    @Relation(parentColumn = "album_id", entityColumn = "id")
    val album: AlbumEntity?,
    @Relation(parentColumn = "id", entityColumn = "track_id")
    val credits: List<TrackArtistCreditEntity>,
    @Relation(
        parentColumn = "id",
        entityColumn = "id",
        associateBy =
        Junction(
            value = TrackArtistCreditEntity::class,
            parentColumn = "track_id",
            entityColumn = "artist_id",
        ),
    )
    val artists: List<ArtistEntity>,
)
