package com.xymusic.app.core.database

import androidx.room.Database
import androidx.room.RoomDatabase
import androidx.room.TypeConverters
import com.xymusic.app.core.database.dao.AccountDataDao
import com.xymusic.app.core.database.dao.CatalogDao
import com.xymusic.app.core.database.dao.CatalogRemoteKeyDao
import com.xymusic.app.core.database.dao.LibraryDao
import com.xymusic.app.core.database.dao.OfflineTrackDao
import com.xymusic.app.core.database.dao.PendingSyncOperationDao
import com.xymusic.app.core.database.dao.PlaybackQueueDao
import com.xymusic.app.core.database.dao.PlaylistDao
import com.xymusic.app.core.database.dao.SearchHistoryDao
import com.xymusic.app.core.database.entity.AlbumArtistCreditEntity
import com.xymusic.app.core.database.entity.AlbumEntity
import com.xymusic.app.core.database.entity.ArtistEntity
import com.xymusic.app.core.database.entity.CatalogRemoteKeyEntity
import com.xymusic.app.core.database.entity.FavoriteEntity
import com.xymusic.app.core.database.entity.HistoryEntity
import com.xymusic.app.core.database.entity.LyricsEntity
import com.xymusic.app.core.database.entity.OfflineTrackEntity
import com.xymusic.app.core.database.entity.PendingSyncOperationEntity
import com.xymusic.app.core.database.entity.PlaybackQueueEntity
import com.xymusic.app.core.database.entity.PlaylistEntity
import com.xymusic.app.core.database.entity.PlaylistEntryEntity
import com.xymusic.app.core.database.entity.SearchHistoryEntity
import com.xymusic.app.core.database.entity.TrackArtistCreditEntity
import com.xymusic.app.core.database.entity.TrackEntity

@Database(
    entities = [
        ArtistEntity::class,
        AlbumEntity::class,
        AlbumArtistCreditEntity::class,
        TrackEntity::class,
        TrackArtistCreditEntity::class,
        LyricsEntity::class,
        FavoriteEntity::class,
        HistoryEntity::class,
        PlaylistEntity::class,
        PlaylistEntryEntity::class,
        PlaybackQueueEntity::class,
        SearchHistoryEntity::class,
        CatalogRemoteKeyEntity::class,
        PendingSyncOperationEntity::class,
        OfflineTrackEntity::class,
    ],
    version = XyMusicDatabase.VERSION,
    exportSchema = true,
)
@TypeConverters(RoomConverters::class)
abstract class XyMusicDatabase : RoomDatabase() {
    abstract fun catalogDao(): CatalogDao

    abstract fun catalogRemoteKeyDao(): CatalogRemoteKeyDao

    abstract fun libraryDao(): LibraryDao

    abstract fun playlistDao(): PlaylistDao

    abstract fun playbackQueueDao(): PlaybackQueueDao

    abstract fun searchHistoryDao(): SearchHistoryDao

    abstract fun pendingSyncOperationDao(): PendingSyncOperationDao

    abstract fun accountDataDao(): AccountDataDao

    abstract fun offlineTrackDao(): OfflineTrackDao

    companion object {
        const val NAME = "xymusic.db"
        const val VERSION = 6
    }
}
