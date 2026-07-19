package com.xymusic.app.core.security

interface TokenVault {
    fun read(): SessionTokens?

    /** On failure, the previously readable session must remain the in-process value. */
    fun write(tokens: SessionTokens)

    /** On failure, subsequent in-process reads must still return null. */
    fun clear()
}
