package com.xymusic.app.app.di

import com.xymusic.app.core.database.PendingAccountCleanupStore
import com.xymusic.app.core.network.auth.RefreshAttemptStore
import com.xymusic.app.core.network.auth.RefreshTokenService
import com.xymusic.app.core.security.EncryptedTokenStorage
import com.xymusic.app.core.security.TokenCipher
import com.xymusic.app.core.security.TokenVault
import com.xymusic.app.data.network.AndroidNetworkEventLogger
import com.xymusic.app.data.network.NetworkEventLogger
import com.xymusic.app.data.network.auth.AuthCallExecutor
import com.xymusic.app.data.network.auth.HttpRefreshTokenService
import com.xymusic.app.data.network.auth.IdempotencyKeyGenerator
import com.xymusic.app.data.network.auth.OkHttpAuthCallExecutor
import com.xymusic.app.data.network.auth.SharedPreferencesRefreshAttemptStore
import com.xymusic.app.data.network.auth.UuidIdempotencyKeyGenerator
import com.xymusic.app.data.security.AndroidKeystoreTokenCipher
import com.xymusic.app.data.security.EncryptedTokenVault
import com.xymusic.app.data.security.SharedPreferencesEncryptedTokenStorage
import com.xymusic.app.data.security.SharedPreferencesPendingAccountCleanupStore
import dagger.Binds
import dagger.Module
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
abstract class SecurityBindingsModule {
    @Binds
    @Singleton
    abstract fun bindTokenCipher(implementation: AndroidKeystoreTokenCipher): TokenCipher

    @Binds
    @Singleton
    abstract fun bindEncryptedTokenStorage(
        implementation: SharedPreferencesEncryptedTokenStorage,
    ): EncryptedTokenStorage

    @Binds
    @Singleton
    abstract fun bindTokenVault(implementation: EncryptedTokenVault): TokenVault

    @Binds
    abstract fun bindAuthCallExecutor(implementation: OkHttpAuthCallExecutor): AuthCallExecutor

    @Binds
    abstract fun bindIdempotencyKeyGenerator(implementation: UuidIdempotencyKeyGenerator): IdempotencyKeyGenerator

    @Binds
    abstract fun bindRefreshTokenService(implementation: HttpRefreshTokenService): RefreshTokenService

    @Binds
    @Singleton
    abstract fun bindRefreshAttemptStore(implementation: SharedPreferencesRefreshAttemptStore): RefreshAttemptStore

    @Binds
    @Singleton
    abstract fun bindNetworkEventLogger(implementation: AndroidNetworkEventLogger): NetworkEventLogger

    @Binds
    @Singleton
    abstract fun bindPendingAccountCleanupStore(
        implementation: SharedPreferencesPendingAccountCleanupStore,
    ): PendingAccountCleanupStore
}
