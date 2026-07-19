package com.xymusic.app.feature.library.domain.model

import com.xymusic.app.core.model.media.Track
import java.util.UUID

data class FavoriteTrack(val track: Track, val favoritedAtEpochMillis: Long)

data class PlaybackHistoryItem(
    val track: Track,
    val lastPositionMs: Long,
    val playCount: Long,
    val lastPlayedAtEpochMillis: Long,
    val completed: Boolean,
    val updatedAtEpochMillis: Long,
)

enum class FavoriteSort {
    FAVORITED_DESC,
    TITLE_ASC,
}

enum class PlaybackEvent {
    STARTED,
    PROGRESS,
    PAUSED,
    COMPLETED,
}

data class PlaybackProgressCommand(
    val trackId: String,
    val playbackSessionId: String,
    val positionMs: Long,
    val occurredAtEpochMillis: Long,
    val event: PlaybackEvent,
) {
    init {
        requireUuid(trackId, "trackId")
        requireUuid(playbackSessionId, "playbackSessionId")
        require(positionMs >= 0) { "positionMs cannot be negative" }
        require(occurredAtEpochMillis > 0) { "occurredAtEpochMillis must be positive" }
    }
}

internal fun requireUuid(value: String, name: String) {
    require(runCatching { UUID.fromString(value) }.isSuccess) { "$name must be a UUID" }
}
