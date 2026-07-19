package com.xymusic.app.app.di

import com.xymusic.app.core.data.media.CatalogLocalDataSource
import com.xymusic.app.core.data.media.RoomCatalogLocalDataSource
import com.xymusic.app.feature.catalog.data.CatalogTransactionRunner
import com.xymusic.app.feature.catalog.data.DefaultCatalogRepository
import com.xymusic.app.feature.catalog.data.RoomCatalogTransactionRunner
import com.xymusic.app.feature.catalog.data.remote.CatalogRemoteDataSource
import com.xymusic.app.feature.catalog.data.remote.HttpCatalogRemoteDataSource
import com.xymusic.app.feature.catalog.domain.CatalogRepository
import dagger.Binds
import dagger.Module
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
abstract class CatalogModule {
    @Binds
    @Singleton
    abstract fun bindCatalogRepository(implementation: DefaultCatalogRepository): CatalogRepository

    @Binds
    @Singleton
    abstract fun bindCatalogLocalDataSource(implementation: RoomCatalogLocalDataSource): CatalogLocalDataSource

    @Binds
    @Singleton
    abstract fun bindCatalogRemoteDataSource(implementation: HttpCatalogRemoteDataSource): CatalogRemoteDataSource

    @Binds
    @Singleton
    abstract fun bindCatalogTransactionRunner(implementation: RoomCatalogTransactionRunner): CatalogTransactionRunner
}
