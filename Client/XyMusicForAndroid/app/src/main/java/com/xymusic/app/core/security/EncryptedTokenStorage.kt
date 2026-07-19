package com.xymusic.app.core.security

interface EncryptedTokenStorage {
    fun read(): String?

    fun write(value: String)

    fun clear()
}
