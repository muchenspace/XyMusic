package com.xymusic.app.feature.auth.domain.model

data class RegistrationResult(val userId: String, val username: String)

data class DeviceInfo(
    val installationId: String,
    val name: String,
    val platform: String = PLATFORM_ANDROID,
    val appVersion: String,
) {
    companion object {
        const val PLATFORM_ANDROID = "ANDROID"
    }
}

data class RegisterCommand(val username: String, val password: String)

data class LoginCommand(val username: String, val password: String)
