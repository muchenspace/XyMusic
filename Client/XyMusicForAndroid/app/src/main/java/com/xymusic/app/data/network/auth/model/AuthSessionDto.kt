package com.xymusic.app.data.network.auth.model

import kotlinx.serialization.Serializable

@Serializable
data class AuthSessionDto(val user: AuthUserDto, val session: AuthSessionInfoDto, val tokens: TokenPairDto)

@Serializable
data class AuthUserDto(
    val id: String,
    val username: String,
    val displayName: String,
    val bio: String?,
    val avatar: ArtworkDto?,
    val role: String,
    val status: String,
    val version: Long,
    val createdAt: String,
    val updatedAt: String,
)

@Serializable
data class AuthSessionInfoDto(val id: String, val deviceName: String, val createdAt: String)

@Serializable
data class ArtworkDto(
    val assetId: String,
    val url: String,
    val cacheKey: String,
    val mimeType: String,
    val expiresAt: String?,
    val width: Int? = null,
    val height: Int? = null,
)

@Serializable
data class TokenPairDto(
    val tokenType: String,
    val accessToken: String,
    val accessTokenExpiresAt: String,
    val refreshToken: String,
    val refreshTokenExpiresAt: String,
)
