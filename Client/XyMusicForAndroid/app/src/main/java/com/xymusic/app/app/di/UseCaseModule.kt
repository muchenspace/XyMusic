package com.xymusic.app.app.di

import com.xymusic.app.domain.server.ServerConfigRepository
import com.xymusic.app.domain.server.ServerConfigUseCases
import com.xymusic.app.domain.settings.AppSettingsRepository
import com.xymusic.app.domain.settings.AppSettingsUseCases
import com.xymusic.app.feature.auth.domain.AuthRepository
import com.xymusic.app.feature.auth.domain.AuthUseCases
import com.xymusic.app.feature.catalog.domain.CatalogRepository
import com.xymusic.app.feature.catalog.domain.CatalogUseCases
import com.xymusic.app.feature.library.domain.LibraryRepository
import com.xymusic.app.feature.library.domain.LibraryUseCases
import com.xymusic.app.feature.player.domain.OfflineTrackRepository
import com.xymusic.app.feature.player.domain.OfflineTrackUseCases
import com.xymusic.app.feature.player.domain.PlaybackQueueStore
import com.xymusic.app.feature.player.domain.PlaybackQueueUseCases
import com.xymusic.app.feature.player.domain.PlayerRepository
import com.xymusic.app.feature.player.domain.PlayerUseCases
import com.xymusic.app.feature.playlist.domain.PlaylistRepository
import com.xymusic.app.feature.playlist.domain.PlaylistUseCases
import com.xymusic.app.feature.search.domain.SearchRepository
import com.xymusic.app.feature.search.domain.SearchUseCases
import com.xymusic.app.feature.settings.domain.ProfileRepository
import com.xymusic.app.feature.settings.domain.ProfileUseCases
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent

/**
 * domain 模块为纯 Kotlin/JVM，不感知 DI 框架；
 * 所有 UseCase 在此 composition root 中显式构造。
 */
@Module
@InstallIn(SingletonComponent::class)
object UseCaseModule {
    @Provides
    fun provideAuthUseCases(repository: AuthRepository): AuthUseCases = AuthUseCases(repository)

    @Provides
    fun provideCatalogUseCases(repository: CatalogRepository): CatalogUseCases = CatalogUseCases(repository)

    @Provides
    fun provideLibraryUseCases(repository: LibraryRepository): LibraryUseCases = LibraryUseCases(repository)

    @Provides
    fun providePlayerUseCases(repository: PlayerRepository): PlayerUseCases = PlayerUseCases(repository)

    @Provides
    fun providePlaylistUseCases(repository: PlaylistRepository): PlaylistUseCases = PlaylistUseCases(repository)

    @Provides
    fun provideSearchUseCases(repository: SearchRepository): SearchUseCases = SearchUseCases(repository)

    @Provides
    fun provideProfileUseCases(repository: ProfileRepository): ProfileUseCases = ProfileUseCases(repository)

    @Provides
    fun provideAppSettingsUseCases(repository: AppSettingsRepository): AppSettingsUseCases =
        AppSettingsUseCases(repository)

    @Provides
    fun provideServerConfigUseCases(repository: ServerConfigRepository): ServerConfigUseCases =
        ServerConfigUseCases(repository)

    @Provides
    fun provideOfflineTrackUseCases(repository: OfflineTrackRepository): OfflineTrackUseCases =
        OfflineTrackUseCases(repository)

    @Provides
    fun providePlaybackQueueUseCases(store: PlaybackQueueStore): PlaybackQueueUseCases = PlaybackQueueUseCases(store)
}
