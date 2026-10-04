package com.xymusic.app.feature.player.domain

import com.xymusic.app.feature.library.domain.model.PlaybackEvent

data class PlaybackCheckpoint(
    val playbackSessionId: String,
    val queueItemId: String,
    val trackId: String,
    val positionMs: Long,
    val durationMs: Long,
    val occurredAtEpochMillis: Long,
    val event: PlaybackEventType,
)

typealias PlaybackEventType = PlaybackEvent

fun interface PlaybackEventSink {
    suspend fun record(ownerUserId: String, checkpoint: PlaybackCheckpoint)
}
