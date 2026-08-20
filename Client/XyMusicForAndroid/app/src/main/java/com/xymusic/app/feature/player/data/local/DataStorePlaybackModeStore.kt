package com.xymusic.app.feature.player.data.local

import android.content.Context
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.emptyPreferences
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import com.xymusic.app.feature.player.domain.PlaybackModePreference
import com.xymusic.app.feature.player.domain.PlaybackModeStore
import com.xymusic.app.feature.player.domain.model.RepeatMode
import dagger.hilt.android.qualifiers.ApplicationContext
import java.io.IOException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map

private val Context.xyMusicPlaybackModeDataStore by preferencesDataStore(name = "xy_music_playback_mode")

@Singleton
class DataStorePlaybackModeStore
@Inject
constructor(@ApplicationContext context: Context) : PlaybackModeStore {
    private val dataStore = context.xyMusicPlaybackModeDataStore

    override suspend fun read(): PlaybackModePreference = dataStore.data
        .catch { failure ->
            if (failure is IOException) emit(emptyPreferences()) else throw failure
        }
        .map(::toPreference)
        .first()

    override suspend fun write(preference: PlaybackModePreference) {
        dataStore.edit { values ->
            values[Keys.REPEAT_MODE] = preference.repeatMode.name
            values[Keys.SHUFFLE_ENABLED] = preference.shuffleEnabled
        }
    }

    private fun toPreference(values: Preferences): PlaybackModePreference = PlaybackModePreference(
        repeatMode = when (values[Keys.REPEAT_MODE]) {
            RepeatMode.ONE.name -> RepeatMode.ONE
            else -> RepeatMode.ALL
        },
        shuffleEnabled = values[Keys.SHUFFLE_ENABLED] ?: false,
    )

    private object Keys {
        val REPEAT_MODE = stringPreferencesKey("repeat_mode")
        val SHUFFLE_ENABLED = booleanPreferencesKey("shuffle_enabled")
    }
}
