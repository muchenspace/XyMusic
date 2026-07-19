package com.xymusic.app.feature.player.data.remote

import com.xymusic.app.feature.player.domain.PlaybackCheckpoint
import com.xymusic.app.feature.player.domain.PlaybackEventSink
import java.io.IOException
import java.time.Instant
import java.util.UUID
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class HttpPlaybackEventSink
@Inject
constructor(private val api: PlaybackApi) : PlaybackEventSink {
    override suspend fun record(ownerUserId: String, checkpoint: PlaybackCheckpoint) {
        require(ownerUserId.isNotBlank())
        validate(checkpoint)
        val response =
            api.recordHistory(
                trackId = checkpoint.trackId,
                idempotencyKey = checkpoint.idempotencyKey(),
                request =
                RecordPlaybackRequestDto(
                    playbackSessionId = checkpoint.playbackSessionId,
                    positionMs = checkpoint.positionMs,
                    occurredAt = Instant.ofEpochMilli(checkpoint.occurredAtEpochMillis).toString(),
                    event = checkpoint.event.name,
                ),
            )
        response.body()?.close()
        if (!response.isSuccessful) {
            response.errorBody()?.close()
            throw IOException("Playback checkpoint was rejected with HTTP ${response.code()}")
        }
    }

    private fun validate(checkpoint: PlaybackCheckpoint) {
        UUID.fromString(checkpoint.playbackSessionId)
        UUID.fromString(checkpoint.trackId)
        require(checkpoint.queueItemId.isNotBlank())
        require(checkpoint.positionMs >= 0)
        require(checkpoint.durationMs >= 0)
        require(checkpoint.occurredAtEpochMillis > 0)
    }

    private fun PlaybackCheckpoint.idempotencyKey(): String = "$playbackSessionId-${event.name}-$occurredAtEpochMillis"
}
