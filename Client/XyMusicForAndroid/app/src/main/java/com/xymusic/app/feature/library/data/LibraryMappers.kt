package com.xymusic.app.feature.library.data

import com.xymusic.app.core.data.media.toDomain
import com.xymusic.app.core.database.entity.HistoryEntity
import com.xymusic.app.core.database.model.PlaybackHistoryReadModel
import com.xymusic.app.core.database.model.TrackSummaryReadModel
import com.xymusic.app.core.model.media.Track
import com.xymusic.app.feature.library.domain.model.PlaybackHistoryItem

internal fun HistoryEntity.toDomain(tracks: Map<String, Track>): PlaybackHistoryItem? {
    val track = tracks[trackId] ?: return null
    return PlaybackHistoryItem(
        track = track,
        lastPositionMs = lastPositionMs,
        playCount = playCount,
        lastPlayedAtEpochMillis = lastPlayedAtEpochMs,
        completed = completed,
        updatedAtEpochMillis = updatedAtEpochMs,
    )
}

internal fun PlaybackHistoryReadModel.toDomain(): PlaybackHistoryItem = PlaybackHistoryItem(
    track = TrackSummaryReadModel(track, album, credits, artists).toDomain(),
    lastPositionMs = lastPositionMs,
    playCount = playCount,
    lastPlayedAtEpochMillis = lastPlayedAtEpochMs,
    completed = completed,
    updatedAtEpochMillis = updatedAtEpochMs,
)
