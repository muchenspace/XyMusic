package com.xymusic.app.domain.settings
import kotlinx.coroutines.flow.Flow

interface AppSettingsRepository {
    val settings: Flow<AppSettings>

    suspend fun update(settings: AppSettings)

    suspend fun mutate(transform: (AppSettings) -> AppSettings)

    suspend fun reset()
}
