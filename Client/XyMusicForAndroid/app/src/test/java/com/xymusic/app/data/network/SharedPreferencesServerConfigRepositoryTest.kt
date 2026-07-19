package com.xymusic.app.data.network

import android.app.Application
import android.content.ContextWrapper
import android.content.SharedPreferences
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.network.ServerEndpoint
import com.xymusic.app.core.network.ServerProtocol
import kotlinx.coroutines.test.runTest
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class SharedPreferencesServerConfigRepositoryTest {
    @Test
    fun endpointSurvivesRepositoryRecreation() = runTest {
        val context = ApplicationProvider.getApplicationContext<Application>()
        context
            .getSharedPreferences(PREFERENCES_NAME, Application.MODE_PRIVATE)
            .edit()
            .clear()
            .commit()
        val expected =
            checkNotNull(
                ServerEndpoint.parse("music.home", "8443", ServerProtocol.HTTPS),
            )

        SharedPreferencesServerConfigRepository(context).update(expected)
        val restoredRepository = SharedPreferencesServerConfigRepository(context)
        restoredRepository.load()

        assertThat(restoredRepository.currentEndpoint()).isEqualTo(expected)
    }

    @Test
    fun missingProtocolDefaultsToHttps() = runTest {
        val context = ApplicationProvider.getApplicationContext<Application>()
        context
            .getSharedPreferences(PREFERENCES_NAME, Application.MODE_PRIVATE)
            .edit()
            .clear()
            .putString(KEY_HOST, "music.home")
            .putInt(KEY_PORT, 8443)
            .commit()

        val restoredRepository = SharedPreferencesServerConfigRepository(context)
        restoredRepository.load()
        val restored = restoredRepository.currentEndpoint()

        assertThat(restored?.protocol).isEqualTo(ServerProtocol.HTTPS)
        assertThat(restored?.displayValue).isEqualTo("https://music.home:8443")
    }

    @Test
    fun explicitHttpSurvivesRepositoryRecreation() = runTest {
        val context = ApplicationProvider.getApplicationContext<Application>()
        context
            .getSharedPreferences(PREFERENCES_NAME, Application.MODE_PRIVATE)
            .edit()
            .clear()
            .commit()
        val expected =
            checkNotNull(
                ServerEndpoint.parse("music.home", "3000", ServerProtocol.HTTP),
            )

        SharedPreferencesServerConfigRepository(context).update(expected)
        val restoredRepository = SharedPreferencesServerConfigRepository(context)
        restoredRepository.load()

        assertThat(restoredRepository.currentEndpoint()).isEqualTo(expected)
    }

    @Test
    fun constructionDoesNotOpenSharedPreferences() {
        val context =
            object : ContextWrapper(
                ApplicationProvider.getApplicationContext<Application>(),
            ) {
                var preferenceOpenCount = 0

                override fun getSharedPreferences(name: String, mode: Int): SharedPreferences {
                    preferenceOpenCount += 1
                    error("Server preferences must be opened on the IO dispatcher")
                }
            }

        SharedPreferencesServerConfigRepository(context)

        assertThat(context.preferenceOpenCount).isEqualTo(0)
    }

    private companion object {
        const val PREFERENCES_NAME = "xy_music_server_config"
        const val KEY_PROTOCOL = "protocol"
        const val KEY_HOST = "host"
        const val KEY_PORT = "port"
    }
}
