package com.xymusic.app.feature.library.data.remote

import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import kotlinx.serialization.Serializable

@Serializable
data class FavoriteItemDto(val track: TrackSummaryDto, val favoritedAt: String)

@Serializable
data class FavoritePageDto(val items: List<FavoriteItemDto>, val nextCursor: String?)

@Serializable
data class HistoryItemDto(
    val track: TrackSummaryDto,
    val lastPositionMs: Long,
    val playCount: Long,
    val lastPlayedAt: String,
    val completed: Boolean,
    val updatedAt: String,
)

@Serializable
data class HistoryPageDto(val items: List<HistoryItemDto>, val nextCursor: String?)

@Serializable
data class RecordPlaybackRequestDto(
    val playbackSessionId: String,
    val positionMs: Long,
    val occurredAt: String,
    val event: String,
)
