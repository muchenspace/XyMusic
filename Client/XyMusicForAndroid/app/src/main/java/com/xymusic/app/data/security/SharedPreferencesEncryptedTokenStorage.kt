package com.xymusic.app.data.security

import android.annotation.SuppressLint
import android.content.Context
import com.xymusic.app.core.security.EncryptedTokenStorage
import dagger.hilt.android.qualifiers.ApplicationContext
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
@SuppressLint("UseKtx") // The KTX helper discards the synchronous commit result checked below.
class SharedPreferencesEncryptedTokenStorage
@Inject
constructor(@ApplicationContext context: Context) :
    EncryptedTokenStorage {
    private val preferences = context.getSharedPreferences(PREFERENCES_NAME, Context.MODE_PRIVATE)

    override fun read(): String? = preferences.getString(KEY_ENCRYPTED_SESSION, null)

    override fun write(value: String) {
        check(preferences.edit().putString(KEY_ENCRYPTED_SESSION, value).commit()) {
            "Unable to persist encrypted session"
        }
    }

    override fun clear() {
        check(preferences.edit().remove(KEY_ENCRYPTED_SESSION).commit()) {
            "Unable to clear encrypted session"
        }
    }

    private companion object {
        const val PREFERENCES_NAME = "secure_session"
        const val KEY_ENCRYPTED_SESSION = "encrypted_session_v1"
    }
}
