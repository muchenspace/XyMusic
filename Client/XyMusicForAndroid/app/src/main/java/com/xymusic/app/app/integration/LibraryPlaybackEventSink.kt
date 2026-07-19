package com.xymusic.app.app.integration

import com.xymusic.app.feature.library.domain.LibraryRepository
import com.xymusic.app.feature.library.domain.LibraryResult
import com.xymusic.app.feature.library.domain.model.PlaybackEvent
import com.xymusic.app.feature.library.domain.model.PlaybackProgressCommand
import com.xymusic.app.feature.player.domain.PlaybackCheckpoint
import com.xymusic.app.feature.player.domain.PlaybackEventSink
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class LibraryPlaybackEventSink
@Inject
constructor(private val libraryRepository: LibraryRepository) :
    PlaybackEventSink {
    override suspend fun record(ownerUserId: String, checkpoint: PlaybackCheckpoint) {
        val result =
            libraryRepository.recordPlaybackForOwner(
                ownerUserId = ownerUserId,
                command =
                PlaybackProgressCommand(
                    trackId = checkpoint.trackId,
                    playbackSessionId = checkpoint.playbackSessionId,
                    positionMs = checkpoint.positionMs,
                    occurredAtEpochMillis = checkpoint.occurredAtEpochMillis,
                    event = PlaybackEvent.valueOf(checkpoint.event.name),
                ),
            )
        check(result is LibraryResult.Success) { "Unable to durably store playback checkpoint" }
    }
}
