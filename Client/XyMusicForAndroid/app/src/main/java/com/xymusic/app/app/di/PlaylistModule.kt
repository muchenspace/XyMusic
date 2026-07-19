package com.xymusic.app.app.di

import com.xymusic.app.feature.playlist.data.DefaultPlaylistRepository
import com.xymusic.app.feature.playlist.data.remote.HttpPlaylistRemoteDataSource
import com.xymusic.app.feature.playlist.data.remote.PlaylistRemoteDataSource
import com.xymusic.app.feature.playlist.domain.PlaylistRepository
import dagger.Binds
import dagger.Module
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
abstract class PlaylistModule {
    @Binds
    @Singleton
    abstract fun bindPlaylistRepository(implementation: DefaultPlaylistRepository): PlaylistRepository

    @Binds
    @Singleton
    abstract fun bindPlaylistRemoteDataSource(implementation: HttpPlaylistRemoteDataSource): PlaylistRemoteDataSource
}
