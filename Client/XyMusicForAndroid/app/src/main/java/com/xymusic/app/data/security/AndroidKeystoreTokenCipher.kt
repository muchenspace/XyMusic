package com.xymusic.app.data.security

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import com.xymusic.app.core.security.EncryptedPayload
import com.xymusic.app.core.security.TokenCipher
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class AndroidKeystoreTokenCipher
@Inject
constructor() : TokenCipher {
    override fun encrypt(plaintext: ByteArray): EncryptedPayload {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, getOrCreateKey())
        return EncryptedPayload(
            initializationVector = cipher.iv.copyOf(),
            ciphertext = cipher.doFinal(plaintext),
        )
    }

    override fun decrypt(payload: EncryptedPayload): ByteArray {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(
            Cipher.DECRYPT_MODE,
            getOrCreateKey(),
            GCMParameterSpec(GCM_TAG_LENGTH_BITS, payload.initializationVector),
        )
        return cipher.doFinal(payload.ciphertext)
    }

    private fun getOrCreateKey(): SecretKey = cachedKey ?: synchronized(keyLock) {
        cachedKey ?: loadOrCreateKey().also { cachedKey = it }
    }

    private fun loadOrCreateKey(): SecretKey {
        val keyStore = KeyStore.getInstance(ANDROID_KEY_STORE).apply { load(null) }
        return (keyStore.getKey(KEY_ALIAS, null) as? SecretKey) ?: generateKey()
    }

    private fun generateKey(): SecretKey {
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEY_STORE)
        val specification =
            KeyGenParameterSpec
                .Builder(
                    KEY_ALIAS,
                    KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
                ).setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(KEY_SIZE_BITS)
                .setRandomizedEncryptionRequired(true)
                .build()
        generator.init(specification)
        return generator.generateKey()
    }

    private companion object {
        const val ANDROID_KEY_STORE = "AndroidKeyStore"
        const val KEY_ALIAS = "xymusic_session_tokens_v1"
        const val TRANSFORMATION = "AES/GCM/NoPadding"
        const val GCM_TAG_LENGTH_BITS = 128
        const val KEY_SIZE_BITS = 256
        val keyLock = Any()
    }

    @Volatile
    private var cachedKey: SecretKey? = null
}
