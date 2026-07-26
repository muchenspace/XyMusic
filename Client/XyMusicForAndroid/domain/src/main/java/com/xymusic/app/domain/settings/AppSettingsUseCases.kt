package com.xymusic.app.domain.settings

class AppSettingsUseCases(private val repository: AppSettingsRepository) {
    val settings = repository.settings

    suspend fun update(settings: AppSettings) = repository.update(settings)

    suspend fun mutate(transform: (AppSettings) -> AppSettings) = repository.mutate(transform)

    suspend fun reset() = repository.reset()
}
