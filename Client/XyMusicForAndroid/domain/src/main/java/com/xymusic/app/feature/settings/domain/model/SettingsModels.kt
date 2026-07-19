package com.xymusic.app.feature.settings.domain.model

import com.xymusic.app.core.model.media.Artwork

data class UserProfile(
    val id: String,
    val username: String,
    val displayName: String,
    val bio: String?,
    val avatar: Artwork?,
    val role: UserRole,
    val status: UserStatus,
    val version: Long,
    val createdAtEpochMillis: Long,
    val updatedAtEpochMillis: Long,
)

enum class UserRole { USER, ADMIN }

enum class UserStatus { ACTIVE, SUSPENDED, DELETED }

sealed interface ProfileValueChange<out T> {
    data object Unchanged : ProfileValueChange<Nothing>

    data class Set<T>(val value: T) : ProfileValueChange<T>
}

data class UpdateProfileCommand(
    val expectedVersion: Long,
    val displayName: ProfileValueChange<String> = ProfileValueChange.Unchanged,
    val bio: ProfileValueChange<String?> = ProfileValueChange.Unchanged,
) {
    init {
        require(expectedVersion >= 1) { "expectedVersion must be positive" }
        require(displayName !is ProfileValueChange.Unchanged || bio !is ProfileValueChange.Unchanged) {
            "At least one profile field must change"
        }
        if (displayName is ProfileValueChange.Set) {
            require(displayName.value.isNotBlank()) { "displayName cannot be blank" }
            require(displayName.value.length <= 64) { "displayName cannot exceed 64 characters" }
        }
        if (bio is ProfileValueChange.Set) {
            require(bio.value == null || bio.value.length <= 500) {
                "bio cannot exceed 500 characters"
            }
        }
    }
}

data class AvatarUploadCommand(val fileName: String, val contentType: String, val content: ByteArray) {
    init {
        require(fileName.isNotBlank()) { "fileName cannot be blank" }
        require(contentType in SUPPORTED_TYPES) { "Unsupported avatar content type" }
        require(content.isNotEmpty() && content.size <= MAX_BYTES) { "Avatar must be 1 byte to 5 MiB" }
    }

    companion object {
        const val MAX_BYTES = 5 * 1024 * 1024
        val SUPPORTED_TYPES = setOf("image/jpeg", "image/png", "image/webp")
    }
}
