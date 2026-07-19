package com.xymusic.app.app.di

import android.content.Context
import androidx.room.Room
import com.xymusic.app.core.database.AccountDataCleaner
import com.xymusic.app.core.database.DatabaseMigrations
import com.xymusic.app.core.database.RoomAccountDataCleaner
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.dao.AccountDataDao
import com.xymusic.app.core.database.dao.CatalogDao
import com.xymusic.app.core.database.dao.CatalogRemoteKeyDao
import com.xymusic.app.core.database.dao.LibraryDao
import com.xymusic.app.core.database.dao.OfflineTrackDao
import com.xymusic.app.core.database.dao.PendingSyncOperationDao
import com.xymusic.app.core.database.dao.PlaybackQueueDao
import com.xymusic.app.core.database.dao.PlaylistDao
import com.xymusic.app.core.database.dao.SearchHistoryDao
import dagger.Binds
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
object DatabaseProviderModule {
    @Provides
    @Singleton
    fun provideDatabase(@ApplicationContext context: Context): XyMusicDatabase = Room
        .databaseBuilder(context, XyMusicDatabase::class.java, XyMusicDatabase.NAME)
        .setJournalMode(androidx.room.RoomDatabase.JournalMode.WRITE_AHEAD_LOGGING)
        .addMigrations(*DatabaseMigrations.ALL)
        .build()

    @Provides
    fun provideCatalogDao(database: XyMusicDatabase): CatalogDao = database.catalogDao()

    @Provides
    fun provideCatalogRemoteKeyDao(database: XyMusicDatabase): CatalogRemoteKeyDao = database.catalogRemoteKeyDao()

    @Provides
    fun provideLibraryDao(database: XyMusicDatabase): LibraryDao = database.libraryDao()

    @Provides
    fun providePlaylistDao(database: XyMusicDatabase): PlaylistDao = database.playlistDao()

    @Provides
    fun providePlaybackQueueDao(database: XyMusicDatabase): PlaybackQueueDao = database.playbackQueueDao()

    @Provides
    fun provideSearchHistoryDao(database: XyMusicDatabase): SearchHistoryDao = database.searchHistoryDao()

    @Provides
    fun providePendingSyncOperationDao(database: XyMusicDatabase): PendingSyncOperationDao =
        database.pendingSyncOperationDao()

    @Provides
    fun provideAccountDataDao(database: XyMusicDatabase): AccountDataDao = database.accountDataDao()

    @Provides
    fun provideOfflineTrackDao(database: XyMusicDatabase): OfflineTrackDao = database.offlineTrackDao()
}

@Module
@InstallIn(SingletonComponent::class)
abstract class DatabaseBindingModule {
    @Binds
    @Singleton
    abstract fun bindAccountDataCleaner(implementation: RoomAccountDataCleaner): AccountDataCleaner
}
