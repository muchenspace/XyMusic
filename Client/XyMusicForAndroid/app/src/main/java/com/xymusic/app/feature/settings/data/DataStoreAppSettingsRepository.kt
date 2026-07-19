package com.xymusic.app.feature.settings.data

import android.content.Context
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.emptyPreferences
import androidx.datastore.preferences.core.intPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import com.xymusic.app.core.preferences.AppSettings
import com.xymusic.app.core.preferences.AppSettingsRepository
import com.xymusic.app.core.preferences.MobileDataPolicy
import com.xymusic.app.core.preferences.StreamingQuality
import com.xymusic.app.core.preferences.ThemePreference
import dagger.hilt.android.qualifiers.ApplicationContext
import java.io.IOException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.map

private val Context.xyMusicSettingsDataStore by preferencesDataStore(name = "xy_music_settings")

@Singleton
class DataStoreAppSettingsRepository
@Inject
constructor(@ApplicationContext context: Context) :
    AppSettingsRepository {
    private val dataStore = context.xyMusicSettingsDataStore

    override val settings: Flow<AppSettings> =
        dataStore.data
            .catch { failure ->
                if (failure is IOException) {
                    emit(
                        androidx.datastore.preferences.core
                            .emptyPreferences(),
                    )
                } else {
                    throw failure
                }
            }.map(::toSettings)

    override suspend fun update(settings: AppSettings) {
        mutate { settings }
    }

    override suspend fun mutate(transform: (AppSettings) -> AppSettings) {
        dataStore.edit { values ->
            val settings = transform(toSettings(values))
            values[Keys.THEME] = settings.theme.name
            values[Keys.DYNAMIC_COLOR_ENABLED] = settings.dynamicColorEnabled
            values[Keys.WORD_BY_WORD_LYRICS_ENABLED] = settings.wordByWordLyricsEnabled
            values[Keys.STREAMING_QUALITY] = settings.streamingQuality.name
            values[Keys.MOBILE_DATA_POLICY] = settings.mobileDataPolicy.name
            values[Keys.CACHE_LIMIT_MIB] = settings.cacheLimitMiB
        }
    }

    override suspend fun reset() {
        dataStore.updateData { emptyPreferences() }
    }

    private fun toSettings(values: Preferences): AppSettings = AppSettings(
        theme = values[Keys.THEME].enumOrDefault(ThemePreference.SYSTEM),
        dynamicColorEnabled = values[Keys.DYNAMIC_COLOR_ENABLED] ?: false,
        wordByWordLyricsEnabled = values[Keys.WORD_BY_WORD_LYRICS_ENABLED] ?: true,
        streamingQuality = values[Keys.STREAMING_QUALITY].enumOrDefault(StreamingQuality.AUTO),
        mobileDataPolicy =
        values[Keys.MOBILE_DATA_POLICY].enumOrDefault(
            MobileDataPolicy.ALLOW_STREAMING,
        ),
        cacheLimitMiB =
        (values[Keys.CACHE_LIMIT_MIB] ?: DEFAULT_CACHE_LIMIT_MIB).coerceIn(
            MIN_CACHE_LIMIT_MIB,
            MAX_CACHE_LIMIT_MIB,
        ),
    )

    private inline fun <reified T : Enum<T>> String?.enumOrDefault(default: T): T =
        this?.let { value -> enumValues<T>().firstOrNull { it.name == value } } ?: default

    private object Keys {
        val THEME = stringPreferencesKey("theme")
        val DYNAMIC_COLOR_ENABLED = booleanPreferencesKey("dynamic_color_enabled")
        val WORD_BY_WORD_LYRICS_ENABLED = booleanPreferencesKey("word_by_word_lyrics_enabled")
        val STREAMING_QUALITY = stringPreferencesKey("streaming_quality")
        val MOBILE_DATA_POLICY = stringPreferencesKey("mobile_data_policy")
        val CACHE_LIMIT_MIB = intPreferencesKey("cache_limit_mib")
    }

    private companion object {
        const val DEFAULT_CACHE_LIMIT_MIB = 512
        const val MIN_CACHE_LIMIT_MIB = 128
        const val MAX_CACHE_LIMIT_MIB = 4_096
    }
}
