package com.xymusic.app.app.di

import com.xymusic.app.BuildConfig
import com.xymusic.app.core.network.ApiBaseUrl
import com.xymusic.app.core.network.ApiCallFactory
import com.xymusic.app.core.network.ApiHttpClient
import com.xymusic.app.core.network.AuthHttpClient
import com.xymusic.app.core.network.MediaHttpClient
import com.xymusic.app.core.network.ServerConfigRepository
import com.xymusic.app.core.network.ServerSynchronizedClock
import com.xymusic.app.data.network.BearerTokenInterceptor
import com.xymusic.app.data.network.ClientMetadataInterceptor
import com.xymusic.app.data.network.NetworkEventLogger
import com.xymusic.app.data.network.RemoveAuthorizationInterceptor
import com.xymusic.app.data.network.SafeNetworkLoggingInterceptor
import com.xymusic.app.data.network.ServerEndpointInterceptor
import com.xymusic.app.data.network.SessionRequestContextBinder
import com.xymusic.app.data.network.SessionRequestContextCallFactory
import com.xymusic.app.data.network.SessionRequestContextInterceptor
import com.xymusic.app.data.network.SessionRequestContextValidationInterceptor
import com.xymusic.app.data.network.auth.RefreshingAuthenticator
import com.xymusic.app.feature.auth.data.remote.PublicAuthApi
import com.xymusic.app.feature.auth.data.remote.SessionAuthApi
import com.xymusic.app.feature.catalog.data.remote.CatalogApi
import com.xymusic.app.feature.library.data.remote.LibraryApi
import com.xymusic.app.feature.player.data.remote.PlaybackApi
import com.xymusic.app.feature.playlist.data.remote.PlaylistApi
import com.xymusic.app.feature.search.data.remote.SearchApi
import com.xymusic.app.feature.settings.data.remote.ProfileApi
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import java.time.Clock
import java.util.concurrent.TimeUnit
import javax.inject.Singleton
import kotlinx.serialization.json.Json
import okhttp3.Call
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

@Module
@InstallIn(SingletonComponent::class)
object NetworkModule {
    @Provides
    @Singleton
    fun provideJson(): Json = Json {
        ignoreUnknownKeys = true
        explicitNulls = false
        encodeDefaults = true
    }

    @Provides
    @Singleton
    fun provideServerSynchronizedClock(): ServerSynchronizedClock = ServerSynchronizedClock()

    @Provides
    @Singleton
    fun provideClock(clock: ServerSynchronizedClock): Clock = clock

    @Provides
    @Singleton
    @ApiBaseUrl
    fun provideApiBaseUrl(serverConfigRepository: ServerConfigRepository): HttpUrl =
        serverConfigRepository.currentEndpoint()?.baseUrl ?: PLACEHOLDER_API_BASE_URL

    @Provides
    @Singleton
    fun provideSafeNetworkLoggingInterceptor(logger: NetworkEventLogger): SafeNetworkLoggingInterceptor =
        SafeNetworkLoggingInterceptor(
            logger = logger,
            enabled = BuildConfig.DEBUG,
        )

    @Provides
    @Singleton
    @AuthHttpClient
    fun provideAuthHttpClient(
        serverEndpointInterceptor: ServerEndpointInterceptor,
        metadataInterceptor: ClientMetadataInterceptor,
        removeAuthorizationInterceptor: RemoveAuthorizationInterceptor,
        loggingInterceptor: SafeNetworkLoggingInterceptor,
    ): OkHttpClient = baseClientBuilder()
        .addInterceptor(serverEndpointInterceptor)
        .addInterceptor(metadataInterceptor)
        .addInterceptor(removeAuthorizationInterceptor)
        .addInterceptor(loggingInterceptor)
        .retryOnConnectionFailure(false)
        .callTimeout(AUTH_CALL_TIMEOUT_SECONDS, TimeUnit.SECONDS)
        .build()

    @Provides
    @Singleton
    @ApiHttpClient
    fun provideApiHttpClient(
        sessionRequestContextInterceptor: SessionRequestContextInterceptor,
        serverEndpointInterceptor: ServerEndpointInterceptor,
        metadataInterceptor: ClientMetadataInterceptor,
        bearerTokenInterceptor: BearerTokenInterceptor,
        sessionRequestContextValidationInterceptor: SessionRequestContextValidationInterceptor,
        loggingInterceptor: SafeNetworkLoggingInterceptor,
        authenticator: RefreshingAuthenticator,
    ): OkHttpClient = baseClientBuilder()
        .addInterceptor(sessionRequestContextInterceptor)
        .addInterceptor(serverEndpointInterceptor)
        .addInterceptor(metadataInterceptor)
        .addInterceptor(bearerTokenInterceptor)
        .addInterceptor(loggingInterceptor)
        .addNetworkInterceptor(sessionRequestContextValidationInterceptor)
        .authenticator(authenticator)
        .callTimeout(API_CALL_TIMEOUT_SECONDS, TimeUnit.SECONDS)
        .build()

    @Provides
    @Singleton
    @ApiCallFactory
    fun provideApiCallFactory(
        @ApiHttpClient client: OkHttpClient,
        contextBinder: SessionRequestContextBinder,
    ): Call.Factory = SessionRequestContextCallFactory(client, contextBinder)

    @Provides
    @Singleton
    @MediaHttpClient
    fun provideMediaHttpClient(removeAuthorizationInterceptor: RemoveAuthorizationInterceptor): OkHttpClient =
        baseClientBuilder()
            .addInterceptor(removeAuthorizationInterceptor)
            .readTimeout(MEDIA_READ_TIMEOUT_SECONDS, TimeUnit.SECONDS)
            .callTimeout(0, TimeUnit.SECONDS)
            .build()

    @Provides
    @Singleton
    fun providePublicAuthApi(
        @ApiBaseUrl apiBaseUrl: HttpUrl,
        @AuthHttpClient client: OkHttpClient,
        json: Json,
    ): PublicAuthApi = retrofit(apiBaseUrl, client, json).create(PublicAuthApi::class.java)

    @Provides
    @Singleton
    fun provideSessionAuthApi(
        @ApiBaseUrl apiBaseUrl: HttpUrl,
        @ApiCallFactory callFactory: Call.Factory,
        json: Json,
    ): SessionAuthApi = retrofit(apiBaseUrl, callFactory, json).create(SessionAuthApi::class.java)

    @Provides
    @Singleton
    fun provideCatalogApi(
        @ApiBaseUrl apiBaseUrl: HttpUrl,
        @ApiCallFactory callFactory: Call.Factory,
        json: Json,
    ): CatalogApi = retrofit(apiBaseUrl, callFactory, json).create(CatalogApi::class.java)

    @Provides
    @Singleton
    fun provideSearchApi(
        @ApiBaseUrl apiBaseUrl: HttpUrl,
        @ApiCallFactory callFactory: Call.Factory,
        json: Json,
    ): SearchApi = retrofit(apiBaseUrl, callFactory, json).create(SearchApi::class.java)

    @Provides
    @Singleton
    fun providePlaybackApi(
        @ApiBaseUrl apiBaseUrl: HttpUrl,
        @ApiCallFactory callFactory: Call.Factory,
        json: Json,
    ): PlaybackApi = retrofit(apiBaseUrl, callFactory, json).create(PlaybackApi::class.java)

    @Provides
    @Singleton
    fun provideLibraryApi(
        @ApiBaseUrl apiBaseUrl: HttpUrl,
        @ApiCallFactory callFactory: Call.Factory,
        json: Json,
    ): LibraryApi = retrofit(apiBaseUrl, callFactory, json).create(LibraryApi::class.java)

    @Provides
    @Singleton
    fun providePlaylistApi(
        @ApiBaseUrl apiBaseUrl: HttpUrl,
        @ApiCallFactory callFactory: Call.Factory,
        json: Json,
    ): PlaylistApi = retrofit(apiBaseUrl, callFactory, json).create(PlaylistApi::class.java)

    @Provides
    @Singleton
    fun provideProfileApi(
        @ApiBaseUrl apiBaseUrl: HttpUrl,
        @ApiCallFactory callFactory: Call.Factory,
        json: Json,
    ): ProfileApi = retrofit(apiBaseUrl, callFactory, json).create(ProfileApi::class.java)

    private fun retrofit(apiBaseUrl: HttpUrl, callFactory: Call.Factory, json: Json): Retrofit = Retrofit
        .Builder()
        .baseUrl(apiBaseUrl)
        .callFactory(callFactory)
        .addConverterFactory(json.asConverterFactory(JSON_MEDIA_TYPE))
        .build()

    private fun baseClientBuilder(): OkHttpClient.Builder = OkHttpClient
        .Builder()
        .connectTimeout(CONNECT_TIMEOUT_SECONDS, TimeUnit.SECONDS)
        .readTimeout(API_READ_TIMEOUT_SECONDS, TimeUnit.SECONDS)
        .writeTimeout(WRITE_TIMEOUT_SECONDS, TimeUnit.SECONDS)
        .retryOnConnectionFailure(true)

    private const val CONNECT_TIMEOUT_SECONDS = 15L
    private const val API_READ_TIMEOUT_SECONDS = 30L
    private const val MEDIA_READ_TIMEOUT_SECONDS = 45L
    private const val WRITE_TIMEOUT_SECONDS = 30L
    private const val AUTH_CALL_TIMEOUT_SECONDS = 30L
    private const val API_CALL_TIMEOUT_SECONDS = 45L
    private val PLACEHOLDER_API_BASE_URL = "https://localhost/".toHttpUrl()
    private val JSON_MEDIA_TYPE = "application/json".toMediaType()
}
