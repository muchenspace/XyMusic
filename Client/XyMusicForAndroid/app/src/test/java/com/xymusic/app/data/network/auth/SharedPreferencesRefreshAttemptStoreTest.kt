package com.xymusic.app.data.network.auth

import android.app.Application
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.security.RefreshToken
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class SharedPreferencesRefreshAttemptStoreTest {
    private val context: Application = ApplicationProvider.getApplicationContext()

    @Before
    fun clearPreferences() {
        context
            .getSharedPreferences("secure_refresh_attempt", Application.MODE_PRIVATE)
            .edit()
            .clear()
            .commit()
    }

    @Test
    fun sameRefreshTokenReusesPersistedKeyAcrossStoreInstances() {
        val sequence = AtomicInteger()
        val generator = IdempotencyKeyGenerator { "key-" + sequence.incrementAndGet() }
        val refreshToken = RefreshToken.from("refresh-token")

        val firstStore = SharedPreferencesRefreshAttemptStore(context, generator)
        val firstKey = firstStore.idempotencyKeyFor(refreshToken)
        val recreatedStore = SharedPreferencesRefreshAttemptStore(context, generator)
        val recoveredKey = recreatedStore.idempotencyKeyFor(refreshToken)

        assertThat(firstKey).isEqualTo("key-1")
        assertThat(recoveredKey).isEqualTo(firstKey)
        assertThat(sequence.get()).isEqualTo(1)
    }

    @Test
    fun rotatedRefreshTokenGetsNewKey() {
        val sequence = AtomicInteger()
        val store =
            SharedPreferencesRefreshAttemptStore(
                context = context,
                keyGenerator = IdempotencyKeyGenerator { "key-" + sequence.incrementAndGet() },
            )

        val firstKey = store.idempotencyKeyFor(RefreshToken.from("refresh-token-1"))
        val secondKey = store.idempotencyKeyFor(RefreshToken.from("refresh-token-2"))

        assertThat(firstKey).isEqualTo("key-1")
        assertThat(secondKey).isEqualTo("key-2")
    }
}
