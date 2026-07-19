package com.xymusic.app.feature.library.data.sync

import kotlinx.serialization.Serializable

@Serializable
data class FavoritePendingPayload(val trackId: String)

@Serializable
data class PlaybackPendingPayload(
    val trackId: String,
    val playbackSessionId: String,
    val positionMs: Long,
    val occurredAt: String,
    val event: String,
)
