package com.xymusic.app.data.network.auth

import android.annotation.SuppressLint
import android.content.Context
import android.util.Base64
import com.xymusic.app.core.network.auth.RefreshAttemptStore
import com.xymusic.app.core.security.RefreshToken
import dagger.hilt.android.qualifiers.ApplicationContext
import java.security.MessageDigest
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
@SuppressLint("UseKtx") // Synchronous commit is required before the refresh request is sent.
class SharedPreferencesRefreshAttemptStore
@Inject
constructor(
    @ApplicationContext context: Context,
    private val keyGenerator: IdempotencyKeyGenerator,
) : RefreshAttemptStore {
    private val preferences = context.getSharedPreferences(PREFERENCES_NAME, Context.MODE_PRIVATE)

    @Synchronized
    override fun idempotencyKeyFor(refreshToken: RefreshToken): String {
        val fingerprint = fingerprint(refreshToken)
        val storedFingerprint = preferences.getString(KEY_TOKEN_FINGERPRINT, null)
        val storedKey = preferences.getString(KEY_IDEMPOTENCY_KEY, null)
        if (storedFingerprint == fingerprint && !storedKey.isNullOrBlank()) {
            return storedKey
        }

        val newKey = keyGenerator.generate()
        check(
            preferences
                .edit()
                .clear()
                .putString(KEY_TOKEN_FINGERPRINT, fingerprint)
                .putString(KEY_IDEMPOTENCY_KEY, newKey)
                .commit(),
        ) {
            "Unable to persist refresh attempt"
        }
        return newKey
    }

    @Synchronized
    override fun clear() {
        check(preferences.edit().clear().commit()) {
            "Unable to clear refresh attempt"
        }
    }

    private fun fingerprint(refreshToken: RefreshToken): String {
        val digest =
            MessageDigest
                .getInstance(SHA_256)
                .digest(refreshToken.value.encodeToByteArray())
        return Base64.encodeToString(digest, Base64.NO_WRAP or Base64.NO_PADDING or Base64.URL_SAFE)
    }

    private companion object {
        const val PREFERENCES_NAME = "secure_refresh_attempt"
        const val KEY_TOKEN_FINGERPRINT = "token_fingerprint"
        const val KEY_IDEMPOTENCY_KEY = "idempotency_key"
        const val SHA_256 = "SHA-256"
    }
}
