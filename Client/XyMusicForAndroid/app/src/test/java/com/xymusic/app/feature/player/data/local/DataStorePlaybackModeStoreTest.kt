package com.xymusic.app.feature.player.data.local

import android.app.Application
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.emptyPreferences
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.player.domain.PlaybackModePreference
import com.xymusic.app.feature.player.domain.model.RepeatMode
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class DataStorePlaybackModeStoreTest {
    @After
    fun clearPersistedMode() = runTest {
        store().rawDataStore().updateData { emptyPreferences() }
    }

    @Test
    fun emptyStoreUsesListRepeatMode() = runTest {
        val store = store().also { it.rawDataStore().updateData { emptyPreferences() } }

        assertThat(store.read()).isEqualTo(PlaybackModePreference())
    }

    @Test
    fun persistedModeSurvivesRepositoryRecreation() = runTest {
        store().write(
            PlaybackModePreference(
                repeatMode = RepeatMode.ONE,
                shuffleEnabled = true,
            ),
        )

        assertThat(store().read()).isEqualTo(
            PlaybackModePreference(
                repeatMode = RepeatMode.ONE,
                shuffleEnabled = true,
            ),
        )
    }

    @Test
    fun invalidStoredRepeatModeFallsBackWithoutDisablingShuffle() = runTest {
        val store = store().also { it.rawDataStore().updateData { emptyPreferences() } }
        store.rawDataStore().edit { values ->
            values[stringPreferencesKey("repeat_mode")] = "UNKNOWN"
            values[booleanPreferencesKey("shuffle_enabled")] = true
        }

        assertThat(store.read()).isEqualTo(
            PlaybackModePreference(
                repeatMode = RepeatMode.ALL,
                shuffleEnabled = true,
            ),
        )
    }

    private fun store(): DataStorePlaybackModeStore = DataStorePlaybackModeStore(
        ApplicationProvider.getApplicationContext<Application>(),
    )

    @Suppress("UNCHECKED_CAST")
    private fun DataStorePlaybackModeStore.rawDataStore(): DataStore<Preferences> = javaClass
        .getDeclaredField("dataStore")
        .apply { isAccessible = true }
        .get(this) as DataStore<Preferences>
}
