package com.xymusic.app.app.di

import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSource
import com.xymusic.app.app.integration.CatalogLyricsSource
import com.xymusic.app.app.integration.LibraryPlaybackEventSink
import com.xymusic.app.core.database.OfflineAccountDataCleaner
import com.xymusic.app.core.network.MediaHttpClient
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.SessionIdentityProvider
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.feature.player.data.controller.Media3PlayerRepository
import com.xymusic.app.feature.player.data.local.RoomPlaybackQueueStore
import com.xymusic.app.feature.player.data.media.CacheOfflineMediaDownloader
import com.xymusic.app.feature.player.data.media.InMemoryPlaybackGrantStore
import com.xymusic.app.feature.player.data.media.Media3OfflineTrackRepository
import com.xymusic.app.feature.player.data.media.OfflineMediaCache
import com.xymusic.app.feature.player.data.media.OfflineMediaDownloader
import com.xymusic.app.feature.player.data.media.OfflineMediaStore
import com.xymusic.app.feature.player.data.media.PlaybackCache
import com.xymusic.app.feature.player.data.media.PlaybackDataSourceFactory
import com.xymusic.app.feature.player.data.media.PlaybackGrantStore
import com.xymusic.app.feature.player.data.media.PlaybackNetworkPolicy
import com.xymusic.app.feature.player.data.media.playbackDataSourceFactory
import com.xymusic.app.feature.player.data.remote.HttpPlaybackGrantRepository
import com.xymusic.app.feature.player.domain.LyricsSource
import com.xymusic.app.feature.player.domain.OfflineTrackRepository
import com.xymusic.app.feature.player.domain.PlaybackEventSink
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import com.xymusic.app.feature.player.domain.PlaybackQueueStore
import com.xymusic.app.feature.player.domain.PlayerRepository
import dagger.Binds
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton
import okhttp3.OkHttpClient

@Module
@InstallIn(SingletonComponent::class)
abstract class PlaybackBindingModule {
    @Binds
    @Singleton
    abstract fun bindPlayerRepository(implementation: Media3PlayerRepository): PlayerRepository

    @Binds
    @Singleton
    abstract fun bindPlaybackGrantRepository(implementation: HttpPlaybackGrantRepository): PlaybackGrantRepository

    @Binds
    @Singleton
    abstract fun bindPlaybackGrantStore(implementation: InMemoryPlaybackGrantStore): PlaybackGrantStore

    @Binds
    @Singleton
    abstract fun bindPlaybackQueueStore(implementation: RoomPlaybackQueueStore): PlaybackQueueStore

    @Binds
    @Singleton
    abstract fun bindPlaybackEventSink(implementation: LibraryPlaybackEventSink): PlaybackEventSink

    @Binds
    @Singleton
    abstract fun bindLyricsSource(implementation: CatalogLyricsSource): LyricsSource

    @Binds
    @Singleton
    @UnstableApi
    abstract fun bindOfflineTrackRepository(implementation: Media3OfflineTrackRepository): OfflineTrackRepository

    @Binds
    @Singleton
    @UnstableApi
    abstract fun bindOfflineMediaCache(implementation: PlaybackCache): OfflineMediaCache

    @Binds
    @Singleton
    @UnstableApi
    abstract fun bindOfflineMediaDownloader(implementation: CacheOfflineMediaDownloader): OfflineMediaDownloader

    @Binds
    @Singleton
    abstract fun bindOfflineAccountDataCleaner(implementation: OfflineMediaStore): OfflineAccountDataCleaner
}

@Module
@InstallIn(SingletonComponent::class)
object PlaybackProviderModule {
    @Provides
    @Singleton
    @PlaybackDataSourceFactory
    @UnstableApi
    fun providePlaybackDataSourceFactory(
        @MediaHttpClient mediaHttpClient: OkHttpClient,
        playbackCache: PlaybackCache,
        grantRepository: PlaybackGrantRepository,
        networkPolicy: PlaybackNetworkPolicy,
        offlineMediaStore: OfflineMediaStore,
        sessionProvider: AppSessionProvider,
        sessionIdentityProvider: SessionIdentityProvider,
        sessionMutationCoordinator: SessionMutationCoordinator,
    ): DataSource.Factory = playbackDataSourceFactory(
        mediaHttpClient,
        playbackCache,
        grantRepository,
        networkPolicy,
        offlineMediaStore,
        sessionProvider,
        sessionIdentityProvider,
        sessionMutationCoordinator,
    )
}
