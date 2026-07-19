package com.xymusic.app.app.di

import androidx.media3.common.util.UnstableApi
import com.xymusic.app.app.ServerCacheCleaner
import com.xymusic.app.app.ServerDataCleaner
import com.xymusic.app.app.session.DefaultAppSessionProvider
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.core.session.SessionInvalidator
import com.xymusic.app.core.session.SessionStateController
import dagger.Binds
import dagger.Module
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
abstract class AppModule {
    @Binds
    @Singleton
    @UnstableApi
    abstract fun bindServerDataCleaner(cleaner: ServerCacheCleaner): ServerDataCleaner

    @Binds
    @Singleton
    abstract fun bindAppSessionProvider(provider: DefaultAppSessionProvider): AppSessionProvider

    @Binds
    @Singleton
    abstract fun bindSessionStateController(provider: DefaultAppSessionProvider): SessionStateController

    @Binds
    @Singleton
    abstract fun bindSessionInvalidator(provider: DefaultAppSessionProvider): SessionInvalidator

    @Binds
    @Singleton
    abstract fun bindSessionIdentityProvider(provider: DefaultAppSessionProvider): SessionIdentityProvider
}
