package com.xymusic.app.feature.auth.data.remote

import com.xymusic.app.feature.auth.domain.model.DeviceInfo
import kotlinx.serialization.Serializable

@Serializable
data class RegisterRequestDto(val username: String, val password: String)

@Serializable
data class RegistrationResultDto(val userId: String, val username: String, val status: String)

@Serializable
data class LoginRequestDto(val username: String, val password: String, val device: DeviceInfoDto)

@Serializable
data class DeviceInfoDto(val installationId: String, val name: String, val platform: String, val appVersion: String)

fun DeviceInfo.toDto(): DeviceInfoDto = DeviceInfoDto(
    installationId = installationId,
    name = name,
    platform = platform,
    appVersion = appVersion,
)

const val ACTIVE_STATUS = "ACTIVE"
