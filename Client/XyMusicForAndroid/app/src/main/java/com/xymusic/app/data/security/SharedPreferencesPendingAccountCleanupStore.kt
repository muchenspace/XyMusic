package com.xymusic.app.data.security

import android.annotation.SuppressLint
import android.content.Context
import com.xymusic.app.core.database.PendingAccountCleanupStore
import dagger.hilt.android.qualifiers.ApplicationContext
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
@SuppressLint("UseKtx") // Cleanup intent must survive a process stop before Room is touched.
class SharedPreferencesPendingAccountCleanupStore
@Inject
constructor(@ApplicationContext context: Context) :
    PendingAccountCleanupStore {
    private val preferences = context.getSharedPreferences(PREFERENCES_NAME, Context.MODE_PRIVATE)

    @Synchronized
    override fun owners(): Set<String> = preferences.getStringSet(KEY_OWNER_USER_IDS, emptySet()).orEmpty().toSet()

    @Synchronized
    override fun add(ownerUserId: String) {
        require(ownerUserId.isNotBlank()) { "Owner user ID cannot be blank" }
        persist(owners() + ownerUserId)
    }

    @Synchronized
    override fun remove(ownerUserId: String) {
        if (ownerUserId.isBlank()) return
        persist(owners() - ownerUserId)
    }

    private fun persist(owners: Set<String>) {
        check(
            preferences
                .edit()
                .putStringSet(KEY_OWNER_USER_IDS, owners)
                .commit(),
        ) {
            "Unable to persist pending account cleanup"
        }
    }

    private companion object {
        const val PREFERENCES_NAME = "pending_account_cleanup"
        const val KEY_OWNER_USER_IDS = "owner_user_ids"
    }
}
