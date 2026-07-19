package com.xymusic.app.feature.settings.data

import android.app.Application
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.intPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.preferences.AppSettings
import com.xymusic.app.core.preferences.MobileDataPolicy
import com.xymusic.app.core.preferences.StreamingQuality
import com.xymusic.app.core.preferences.ThemePreference
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class DataStoreAppSettingsRepositoryTest {
    @After
    fun clearPersistedSettings() = runTest {
        repository().reset()
    }

    @Test
    fun emptyStoreUsesSystemTheme() = runTest {
        val repository = repository().also { it.reset() }

        val settings = repository.settings.first()

        assertThat(settings.theme).isEqualTo(ThemePreference.SYSTEM)
        assertThat(settings.wordByWordLyricsEnabled).isTrue()
    }

    @Test
    fun persistedEnumNamesRemainReadable() = runTest {
        val repository = repository().also { it.reset() }
        ThemePreference.entries.forEach { theme ->
            repository.rawDataStore().edit { values ->
                values[stringPreferencesKey("theme")] = theme.name
            }
            assertThat(repository.settings.first().theme).isEqualTo(theme)
        }
        StreamingQuality.entries.forEach { quality ->
            repository.rawDataStore().edit { values ->
                values[stringPreferencesKey("streaming_quality")] = quality.name
            }
            assertThat(repository.settings.first().streamingQuality).isEqualTo(quality)
        }
        MobileDataPolicy.entries.forEach { policy ->
            repository.rawDataStore().edit { values ->
                values[stringPreferencesKey("mobile_data_policy")] = policy.name
            }
            assertThat(repository.settings.first().mobileDataPolicy).isEqualTo(policy)
        }
    }

    @Test
    fun everySettingSurvivesRepositoryRecreation() = runTest {
        val repository = repository().also { it.reset() }
        val expected =
            AppSettings(
                theme = ThemePreference.TWILIGHT_PURPLE,
                dynamicColorEnabled = true,
                wordByWordLyricsEnabled = false,
                streamingQuality = StreamingQuality.LOSSLESS,
                mobileDataPolicy = MobileDataPolicy.WIFI_ONLY,
                cacheLimitMiB = 4_096,
            )

        repository.update(expected)

        assertThat(repository().settings.first()).isEqualTo(expected)
    }

    @Test
    fun resetRestoresEveryDefaultSetting() = runTest {
        val repository = repository().also { it.reset() }
        repository.update(
            AppSettings(
                theme = ThemePreference.OCEAN_BLUE,
                dynamicColorEnabled = true,
                wordByWordLyricsEnabled = false,
                streamingQuality = StreamingQuality.HIGH,
                mobileDataPolicy = MobileDataPolicy.WIFI_ONLY,
                cacheLimitMiB = 2_048,
            ),
        )

        repository.reset()

        assertThat(repository.settings.first()).isEqualTo(AppSettings())
    }

    @Test
    fun unknownEnumsFallBackAndCacheLimitIsClamped() = runTest {
        val repository = repository().also { it.reset() }
        repository.rawDataStore().edit { values ->
            values[stringPreferencesKey("theme")] = "UNKNOWN_THEME"
            values[stringPreferencesKey("streaming_quality")] = "ULTRA"
            values[stringPreferencesKey("mobile_data_policy")] = "CELLULAR_ONLY"
            values[intPreferencesKey("cache_limit_mib")] = Int.MIN_VALUE
        }

        assertThat(repository.settings.first())
            .isEqualTo(AppSettings(cacheLimitMiB = 128))

        repository.rawDataStore().edit { values ->
            values[intPreferencesKey("cache_limit_mib")] = Int.MAX_VALUE
        }

        assertThat(repository.settings.first().cacheLimitMiB).isEqualTo(4_096)
    }

    @Test
    fun persistedWordByWordLyricsPreferenceRemainsReadable() = runTest {
        val repository = repository().also { it.reset() }
        repository.rawDataStore().edit { values ->
            values[booleanPreferencesKey("word_by_word_lyrics_enabled")] = false
        }

        assertThat(repository().settings.first().wordByWordLyricsEnabled).isFalse()
    }

    @Test
    fun concurrentFieldMutationsPreserveBothUpdates() = runTest {
        val repository = repository().also { it.reset() }

        listOf(
            async { repository.mutate { it.copy(theme = ThemePreference.DARK) } },
            async { repository.mutate { it.copy(dynamicColorEnabled = true) } },
            async { repository.mutate { it.copy(wordByWordLyricsEnabled = false) } },
        ).awaitAll()

        val settings = repository.settings.first()
        assertThat(settings.theme).isEqualTo(ThemePreference.DARK)
        assertThat(settings.dynamicColorEnabled).isTrue()
        assertThat(settings.wordByWordLyricsEnabled).isFalse()
    }

    private fun repository(): DataStoreAppSettingsRepository = DataStoreAppSettingsRepository(
        ApplicationProvider.getApplicationContext<Application>(),
    )

    @Suppress("UNCHECKED_CAST")
    private fun DataStoreAppSettingsRepository.rawDataStore(): DataStore<Preferences> = javaClass
        .getDeclaredField("dataStore")
        .apply { isAccessible = true }
        .get(this) as DataStore<Preferences>
}
