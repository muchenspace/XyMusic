package com.xymusic.app.app.di

import com.xymusic.app.core.sync.PendingSyncOperationHandler
import com.xymusic.app.core.sync.PendingSyncScheduler
import com.xymusic.app.data.sync.WorkManagerPendingSyncScheduler
import com.xymusic.app.feature.library.data.sync.LibraryPendingSyncHandler
import dagger.Binds
import dagger.Module
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import dagger.multibindings.IntoSet
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
abstract class SyncModule {
    @Binds
    @IntoSet
    abstract fun bindLibraryPendingSyncHandler(implementation: LibraryPendingSyncHandler): PendingSyncOperationHandler

    @Binds
    @Singleton
    abstract fun bindPendingSyncScheduler(implementation: WorkManagerPendingSyncScheduler): PendingSyncScheduler
}
