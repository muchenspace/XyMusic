package com.xymusic.app.app.di

import com.xymusic.app.feature.library.data.DefaultLibraryRepository
import com.xymusic.app.feature.library.data.remote.HttpLibraryRemoteDataSource
import com.xymusic.app.feature.library.data.remote.LibraryRemoteDataSource
import com.xymusic.app.feature.library.domain.LibraryRepository
import dagger.Binds
import dagger.Module
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
abstract class LibraryModule {
    @Binds
    @Singleton
    abstract fun bindLibraryRepository(implementation: DefaultLibraryRepository): LibraryRepository

    @Binds
    @Singleton
    abstract fun bindLibraryRemoteDataSource(implementation: HttpLibraryRemoteDataSource): LibraryRemoteDataSource
}
