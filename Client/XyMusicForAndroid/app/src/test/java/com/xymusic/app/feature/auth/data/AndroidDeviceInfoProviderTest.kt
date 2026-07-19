package com.xymusic.app.feature.auth.data

import android.app.Application
import android.content.ContextWrapper
import android.content.SharedPreferences
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import java.util.UUID
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class AndroidDeviceInfoProviderTest {
    private val context: Application
        get() = ApplicationProvider.getApplicationContext()

    @Before
    fun clearPreferences() {
        context
            .getSharedPreferences("auth_device_info", Application.MODE_PRIVATE)
            .edit()
            .clear()
            .commit()
    }

    @Test
    fun installationIdIsValidAndStableAcrossProviderRecreation() {
        val first = AndroidDeviceInfoProvider(context).get()
        val second = AndroidDeviceInfoProvider(context).get()

        assertThat(UUID.fromString(first.installationId).toString())
            .isEqualTo(first.installationId)
        assertThat(second.installationId).isEqualTo(first.installationId)
        assertThat(first.platform).isEqualTo("ANDROID")
        assertThat(first.name).isNotEmpty()
        assertThat(first.appVersion).isNotEmpty()
    }

    @Test
    fun constructionDoesNotOpenSharedPreferences() {
        val wrappedContext =
            object : ContextWrapper(context) {
                var preferenceOpenCount = 0

                override fun getSharedPreferences(name: String, mode: Int): SharedPreferences {
                    preferenceOpenCount += 1
                    error("Device preferences must be opened lazily")
                }
            }

        AndroidDeviceInfoProvider(wrappedContext)

        assertThat(wrappedContext.preferenceOpenCount).isEqualTo(0)
    }
}
