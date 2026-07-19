package com.xymusic.app.data.security

import com.xymusic.app.core.security.AccessToken
import com.xymusic.app.core.security.EncryptedPayload
import com.xymusic.app.core.security.EncryptedTokenStorage
import com.xymusic.app.core.security.RefreshToken
import com.xymusic.app.core.security.SessionTokens
import com.xymusic.app.core.security.TokenCipher
import com.xymusic.app.core.security.TokenVault
import java.util.Base64
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

@Singleton
class EncryptedTokenVault
@Inject
constructor(
    private val storage: EncryptedTokenStorage,
    private val cipher: TokenCipher,
    private val json: Json,
) : TokenVault {
    override fun read(): SessionTokens? {
        if (loaded) return cachedTokens
        return loadOnce()
    }

    @Synchronized
    private fun loadOnce(): SessionTokens? {
        if (loaded) return cachedTokens
        val encodedEnvelope =
            try {
                storage.read()
            } catch (_: Exception) {
                return discardUnreadableSession()
            }
        if (encodedEnvelope == null) {
            cachedTokens = null
            loaded = true
            return null
        }
        cachedTokens =
            try {
                val envelope = json.decodeFromString<VaultEnvelopeDto>(encodedEnvelope)
                require(envelope.schemaVersion == CURRENT_SCHEMA_VERSION)
                val plaintext =
                    cipher.decrypt(
                        EncryptedPayload(
                            initializationVector = decoder.decode(envelope.initializationVector),
                            ciphertext = decoder.decode(envelope.ciphertext),
                        ),
                    )
                json.decodeFromString<PersistedSessionDto>(plaintext.decodeToString()).toDomain()
            } catch (_: Exception) {
                discardUnreadableSession()
            }
        loaded = true
        return cachedTokens
    }

    @Synchronized
    override fun write(tokens: SessionTokens) {
        val previousTokens = read()
        val previousEnvelope = storage.read()
        try {
            val plaintext = json.encodeToString(PersistedSessionDto.from(tokens)).encodeToByteArray()
            val payload = cipher.encrypt(plaintext)
            val envelope =
                VaultEnvelopeDto(
                    schemaVersion = CURRENT_SCHEMA_VERSION,
                    initializationVector = encoder.encodeToString(payload.initializationVector),
                    ciphertext = encoder.encodeToString(payload.ciphertext),
                )
            storage.write(json.encodeToString(envelope))
            cachedTokens = tokens
            loaded = true
        } catch (failure: Exception) {
            runCatching {
                if (previousEnvelope == null) storage.clear() else storage.write(previousEnvelope)
            }.exceptionOrNull()?.let(failure::addSuppressed)
            cachedTokens = previousTokens
            loaded = true
            throw failure
        }
    }

    @Synchronized
    override fun clear() {
        cachedTokens = null
        loaded = true
        storage.clear()
    }

    private fun discardUnreadableSession(): SessionTokens? {
        cachedTokens = null
        loaded = true
        runCatching(storage::clear)
        return null
    }

    @Volatile
    private var cachedTokens: SessionTokens? = null

    @Volatile
    private var loaded = false

    private companion object {
        const val CURRENT_SCHEMA_VERSION = 1
        val encoder: Base64.Encoder = Base64.getEncoder()
        val decoder: Base64.Decoder = Base64.getDecoder()
    }
}

@Serializable
private data class VaultEnvelopeDto(val schemaVersion: Int, val initializationVector: String, val ciphertext: String)

@Serializable
private data class PersistedSessionDto(
    val userId: String,
    val sessionId: String,
    val accessToken: String,
    val accessTokenExpiresAtEpochMillis: Long,
    val refreshToken: String,
    val refreshTokenExpiresAtEpochMillis: Long,
) {
    fun toDomain(): SessionTokens = SessionTokens(
        userId = userId,
        sessionId = sessionId,
        accessToken = AccessToken.from(accessToken),
        accessTokenExpiresAtEpochMillis = accessTokenExpiresAtEpochMillis,
        refreshToken = RefreshToken.from(refreshToken),
        refreshTokenExpiresAtEpochMillis = refreshTokenExpiresAtEpochMillis,
    )

    companion object {
        fun from(tokens: SessionTokens): PersistedSessionDto = PersistedSessionDto(
            userId = tokens.userId,
            sessionId = tokens.sessionId,
            accessToken = tokens.accessToken.value,
            accessTokenExpiresAtEpochMillis = tokens.accessTokenExpiresAtEpochMillis,
            refreshToken = tokens.refreshToken.value,
            refreshTokenExpiresAtEpochMillis = tokens.refreshTokenExpiresAtEpochMillis,
        )
    }
}
