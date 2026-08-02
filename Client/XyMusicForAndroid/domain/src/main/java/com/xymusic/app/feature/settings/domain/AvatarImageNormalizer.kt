package com.xymusic.app.feature.settings.domain

import com.xymusic.app.feature.settings.domain.model.AvatarUploadCommand
import java.io.InputStream

/** A platform-neutral, reopenable input for an avatar selected by the user. */
fun interface AvatarImageSource {
    fun openInputStream(): InputStream?
}

interface AvatarImageNormalizer {
    fun normalize(source: AvatarImageSource): AvatarUploadCommand
}

sealed class AvatarImageException(message: String, cause: Throwable? = null) : Exception(message, cause)

class InvalidAvatarImageException(cause: Throwable? = null) :
    AvatarImageException("The selected file is not a supported image", cause)

class AvatarImageTooLargeException(cause: Throwable? = null) :
    AvatarImageException("The normalized avatar exceeds the upload limit", cause)
