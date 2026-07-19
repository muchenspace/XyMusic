package com.xymusic.app.app.di

import com.xymusic.app.core.sync.PendingSyncScheduler
import com.xymusic.app.data.sync.WorkManagerPendingSyncScheduler
import dagger.Binds
import dagger.Module
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
abstract class SyncModule {
    @Binds
    @Singleton
    abstract fun bindPendingSyncScheduler(implementation: WorkManagerPendingSyncScheduler): PendingSyncScheduler
}
