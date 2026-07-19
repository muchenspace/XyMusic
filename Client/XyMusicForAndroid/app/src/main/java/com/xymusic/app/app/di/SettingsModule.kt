package com.xymusic.app.app.di

import com.xymusic.app.core.preferences.AppSettingsRepository
import com.xymusic.app.feature.settings.data.DataStoreAppSettingsRepository
import com.xymusic.app.feature.settings.data.DefaultProfileRepository
import com.xymusic.app.feature.settings.domain.ProfileRepository
import dagger.Binds
import dagger.Module
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
abstract class SettingsModule {
    @Binds
    @Singleton
    abstract fun bindProfileRepository(implementation: DefaultProfileRepository): ProfileRepository

    @Binds
    @Singleton
    abstract fun bindAppSettingsRepository(implementation: DataStoreAppSettingsRepository): AppSettingsRepository
}
