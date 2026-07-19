package com.xymusic.app.feature.player.domain

data class PlaybackCheckpoint(
    val playbackSessionId: String,
    val queueItemId: String,
    val trackId: String,
    val positionMs: Long,
    val durationMs: Long,
    val occurredAtEpochMillis: Long,
    val event: PlaybackEventType,
)

enum class PlaybackEventType {
    STARTED,
    PROGRESS,
    PAUSED,
    COMPLETED,
}

fun interface PlaybackEventSink {
    suspend fun record(ownerUserId: String, checkpoint: PlaybackCheckpoint)
}
