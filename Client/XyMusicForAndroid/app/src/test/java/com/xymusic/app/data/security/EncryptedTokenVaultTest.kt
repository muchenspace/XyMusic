package com.xymusic.app.data.security

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.security.AccessToken
import com.xymusic.app.core.security.EncryptedPayload
import com.xymusic.app.core.security.EncryptedTokenStorage
import com.xymusic.app.core.security.RefreshToken
import com.xymusic.app.core.security.SessionTokens
import com.xymusic.app.core.security.TokenCipher
import kotlinx.serialization.json.Json
import org.junit.Test

class EncryptedTokenVaultTest {
    private val json = Json { ignoreUnknownKeys = true }

    @Test
    fun roundTripStoresOnlyCiphertext() {
        val storage = MemoryEncryptedStorage()
        val vault = EncryptedTokenVault(storage, ReversingCipher(), json)
        val tokens = tokens()

        vault.write(tokens)

        assertThat(storage.value).doesNotContain(tokens.accessToken.value)
        assertThat(storage.value).doesNotContain(tokens.refreshToken.value)
        assertThat(vault.read()).isEqualTo(tokens)
    }

    @Test
    fun corruptEnvelopeIsDiscarded() {
        val storage = MemoryEncryptedStorage("not-json")
        val vault = EncryptedTokenVault(storage, ReversingCipher(), json)

        val result = vault.read()

        assertThat(result).isNull()
        assertThat(storage.value).isNull()
    }

    @Test
    fun clearFailureStillEvictsTokensFromMemory() {
        val storage = MemoryEncryptedStorage()
        val vault = EncryptedTokenVault(storage, ReversingCipher(), json)
        vault.write(tokens())
        storage.failOnClear = true

        val failure = runCatching(vault::clear).exceptionOrNull()

        assertThat(failure).isNotNull()
        assertThat(vault.read()).isNull()
    }

    @Test
    fun failedWritePreservesPreviouslyReadableSession() {
        val storage = MemoryEncryptedStorage()
        val vault = EncryptedTokenVault(storage, ReversingCipher(), json)
        val previousTokens = tokens()
        val replacementTokens =
            previousTokens.copy(
                userId = "user-2",
                sessionId = "session-2",
                accessToken = AccessToken.from("replacement-access-secret-value"),
                refreshToken = RefreshToken.from("replacement-refresh-secret-value"),
            )
        vault.write(previousTokens)
        storage.failNextWriteAfterMutation = true

        val failure = runCatching { vault.write(replacementTokens) }.exceptionOrNull()

        assertThat(failure).isNotNull()
        assertThat(vault.read()).isEqualTo(previousTokens)
        assertThat(EncryptedTokenVault(storage, ReversingCipher(), json).read())
            .isEqualTo(previousTokens)
    }

    @Test
    fun unreadableSessionRemainsFailClosedWhenStorageCannotBeCleared() {
        val storage = MemoryEncryptedStorage("not-json").apply { failOnClear = true }
        val vault = EncryptedTokenVault(storage, ReversingCipher(), json)

        assertThat(vault.read()).isNull()
        assertThat(vault.read()).isNull()
        assertThat(storage.clearAttempts).isEqualTo(1)
    }

    private fun tokens(): SessionTokens = SessionTokens(
        userId = "user-1",
        sessionId = "session-1",
        accessToken = AccessToken.from("access-secret-value"),
        accessTokenExpiresAtEpochMillis = 1_800_000_000_000,
        refreshToken = RefreshToken.from("refresh-secret-value"),
        refreshTokenExpiresAtEpochMillis = 1_900_000_000_000,
    )
}

private class MemoryEncryptedStorage(initialValue: String? = null) : EncryptedTokenStorage {
    var value: String? = initialValue
    var failOnClear: Boolean = false
    var failNextWriteAfterMutation: Boolean = false
    var clearAttempts: Int = 0

    override fun read(): String? = value

    override fun write(value: String) {
        this.value = value
        if (failNextWriteAfterMutation) {
            failNextWriteAfterMutation = false
            throw IllegalStateException("storage unavailable")
        }
    }

    override fun clear() {
        clearAttempts += 1
        if (failOnClear) throw IllegalStateException("storage unavailable")
        value = null
    }
}

private class ReversingCipher : TokenCipher {
    override fun encrypt(plaintext: ByteArray): EncryptedPayload = EncryptedPayload(
        initializationVector = byteArrayOf(1, 2, 3),
        ciphertext = plaintext.reversedArray(),
    )

    override fun decrypt(payload: EncryptedPayload): ByteArray = payload.ciphertext.reversedArray()
}
