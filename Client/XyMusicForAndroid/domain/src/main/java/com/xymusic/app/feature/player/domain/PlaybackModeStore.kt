package com.xymusic.app.feature.player.domain

import com.xymusic.app.feature.player.domain.model.RepeatMode

data class PlaybackModePreference(val repeatMode: RepeatMode = RepeatMode.ALL, val shuffleEnabled: Boolean = false)

interface PlaybackModeStore {
    suspend fun read(): PlaybackModePreference

    suspend fun write(preference: PlaybackModePreference)
}
