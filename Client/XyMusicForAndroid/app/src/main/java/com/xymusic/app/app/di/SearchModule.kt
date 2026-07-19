package com.xymusic.app.app.di

import com.xymusic.app.feature.search.data.DefaultSearchRepository
import com.xymusic.app.feature.search.data.RoomSearchOverviewStore
import com.xymusic.app.feature.search.data.SearchOverviewStore
import com.xymusic.app.feature.search.data.remote.HttpSearchRemoteDataSource
import com.xymusic.app.feature.search.data.remote.SearchRemoteDataSource
import com.xymusic.app.feature.search.domain.SearchRepository
import dagger.Binds
import dagger.Module
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
abstract class SearchModule {
    @Binds
    @Singleton
    abstract fun bindSearchRepository(implementation: DefaultSearchRepository): SearchRepository

    @Binds
    @Singleton
    abstract fun bindSearchRemoteDataSource(implementation: HttpSearchRemoteDataSource): SearchRemoteDataSource

    @Binds
    @Singleton
    abstract fun bindSearchOverviewStore(implementation: RoomSearchOverviewStore): SearchOverviewStore
}
