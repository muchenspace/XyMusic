package com.xymusic.app.core.model.playlist

enum class PlaylistVisibility {
    PRIVATE,
    UNLISTED,
    PUBLIC,
    ;

    companion object {
        fun fromWireValue(value: String): PlaylistVisibility = when (value) {
            "PRIVATE" -> PRIVATE
            "UNLISTED" -> UNLISTED
            "PUBLIC" -> PUBLIC
            else -> throw IllegalArgumentException("Unknown playlist visibility: $value")
        }
    }
}
