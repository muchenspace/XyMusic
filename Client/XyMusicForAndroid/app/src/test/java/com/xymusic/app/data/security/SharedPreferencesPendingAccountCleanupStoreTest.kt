package com.xymusic.app.data.security

import android.app.Application
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class SharedPreferencesPendingAccountCleanupStoreTest {
    private val context: Application
        get() = ApplicationProvider.getApplicationContext()

    @Before
    fun clearPreferences() {
        context
            .getSharedPreferences("pending_account_cleanup", Application.MODE_PRIVATE)
            .edit()
            .clear()
            .commit()
    }

    @Test
    fun pendingOwnersSurviveStoreRecreationUntilRemoved() {
        SharedPreferencesPendingAccountCleanupStore(context).apply {
            add("user-a")
            add("user-b")
        }

        val restored = SharedPreferencesPendingAccountCleanupStore(context)
        assertThat(restored.owners()).containsExactly("user-a", "user-b")

        restored.remove("user-a")
        assertThat(SharedPreferencesPendingAccountCleanupStore(context).owners())
            .containsExactly("user-b")
    }
}
