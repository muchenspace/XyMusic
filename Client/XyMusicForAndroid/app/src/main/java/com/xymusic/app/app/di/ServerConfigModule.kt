package com.xymusic.app.app.di

import com.xymusic.app.core.network.ServerConfigRepository
import com.xymusic.app.data.network.SharedPreferencesServerConfigRepository
import dagger.Binds
import dagger.Module
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
abstract class ServerConfigModule {
    @Binds
    @Singleton
    abstract fun bindServerConfigRepository(
        implementation: SharedPreferencesServerConfigRepository,
    ): ServerConfigRepository
}
