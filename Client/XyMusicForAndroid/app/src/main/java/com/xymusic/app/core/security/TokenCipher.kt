package com.xymusic.app.core.security

data class EncryptedPayload(val initializationVector: ByteArray, val ciphertext: ByteArray)

interface TokenCipher {
    fun encrypt(plaintext: ByteArray): EncryptedPayload

    fun decrypt(payload: EncryptedPayload): ByteArray
}
