package com.xymusic.app.app.di

import com.xymusic.app.feature.auth.data.AndroidDeviceInfoProvider
import com.xymusic.app.feature.auth.data.DefaultAuthRepository
import com.xymusic.app.feature.auth.domain.AuthRepository
import com.xymusic.app.feature.auth.domain.DeviceInfoProvider
import dagger.Binds
import dagger.Module
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
abstract class AuthModule {
    @Binds
    @Singleton
    abstract fun bindAuthRepository(implementation: DefaultAuthRepository): AuthRepository

    @Binds
    @Singleton
    abstract fun bindDeviceInfoProvider(implementation: AndroidDeviceInfoProvider): DeviceInfoProvider
}
