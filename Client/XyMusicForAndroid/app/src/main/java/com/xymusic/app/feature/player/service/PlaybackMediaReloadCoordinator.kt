package com.xymusic.app.feature.player.service

import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import com.xymusic.app.feature.player.adapter.media3.PlaybackMediaUri
import com.xymusic.app.feature.player.adapter.media3.PlaybackSessionCommands
import com.xymusic.app.feature.player.adapter.media3.playbackRequestedStartPositionMs
import com.xymusic.app.feature.player.adapter.media3.playbackSourceOffsetMs
import com.xymusic.app.feature.player.adapter.media3.playbackStreamProtocol
import com.xymusic.app.feature.player.adapter.media3.withPlaybackResolution
import com.xymusic.app.feature.player.adapter.media3.withoutPlaybackResolution
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import com.xymusic.app.feature.player.domain.PlaybackStreamProtocol
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/**
 * Keeps the player-facing position in the track's coordinate system. HLS
 * reloads are represented by a fresh canonical xymusic:// item so the media
 * source factory can request a grant whose first segment starts at the target.
 */
@UnstableApi
internal class PlaybackMediaReloadCoordinator(
    private val player: Player,
    private val grantRepository: PlaybackGrantRepository,
    private val scope: CoroutineScope,
) : Player.Listener {
    private val mutationMutex = Mutex()
    private var internalPlayerOperation = false
    private var reloadJob: Job? = null

    suspend fun seekTo(queueItemId: String, globalPositionMs: Long): Boolean =
        mutationMutex.withLock {
            if (globalPositionMs < 0) return@withLock false
            val index = indexOf(queueItemId)
            if (index < 0) return@withLock false
            val item = player.getMediaItemAt(index)
            if (item.playbackStreamProtocol() == PlaybackStreamProtocol.HLS) {
                reloadJob?.cancel()
                coroutineScope {
                    reloadJob = launch {
                        reloadOrSeek(
                            mediaItemIndex = index,
                            globalPositionMs = globalPositionMs,
                            forceRefresh = false,
                            playWhenReady = player.playWhenReady,
                        )
                    }
                    reloadJob?.join()
                }
                true
            } else {
                reloadOrSeek(index, globalPositionMs, false, player.playWhenReady)
            }
        }

    fun reloadCurrent(
        globalPositionMs: Long,
        forceRefresh: Boolean,
        playWhenReady: Boolean = player.playWhenReady,
    ): Job {
        reloadJob?.cancel() ?: Unit
        val job = scope.launch {
            mutationMutex.withLock {
                val index = player.currentMediaItemIndex
                    .takeIf { it in 0 until player.mediaItemCount }
                    ?: return@withLock
                reloadOrSeek(index, globalPositionMs, forceRefresh, playWhenReady)
            }
        }
        reloadJob = job
        return job
    }

    override fun onPositionDiscontinuity(
        oldPosition: Player.PositionInfo,
        newPosition: Player.PositionInfo,
        reason: Int,
    ) {
        if (internalPlayerOperation || reason != Player.DISCONTINUITY_REASON_SEEK) return
        val index = newPosition.mediaItemIndex.takeIf { it in 0 until player.mediaItemCount } ?: return
        val item = player.getMediaItemAt(index)
        if (item.playbackStreamProtocol() != PlaybackStreamProtocol.HLS) return
        // Media3 reports the seek before the new HLS source is resolved. The
        // app-level seekTo is the single entry point that requests the directed
        // grant; reacting here would start a second reload and loop.
    }

    private fun indexOf(queueItemId: String): Int =
        (0 until player.mediaItemCount).firstOrNull { player.getMediaItemAt(it).mediaId == queueItemId } ?: -1

    private suspend fun reloadOrSeek(
        mediaItemIndex: Int,
        globalPositionMs: Long,
        forceRefresh: Boolean,
        playWhenReady: Boolean,
    ): Boolean {
        val item = player.getMediaItemAt(mediaItemIndex)
        return when (item.playbackStreamProtocol()) {
            PlaybackStreamProtocol.HLS ->
                reprepare(
                    mediaItemIndex = mediaItemIndex,
                    item = item,
                    protocol = PlaybackStreamProtocol.HLS,
                    globalPositionMs = globalPositionMs,
                    playWhenReady = playWhenReady,
                    forceRefresh = forceRefresh,
                )
            PlaybackStreamProtocol.PROGRESSIVE ->
                if (forceRefresh) {
                    reprepare(
                        mediaItemIndex = mediaItemIndex,
                        item = item,
                        protocol = PlaybackStreamProtocol.PROGRESSIVE,
                        globalPositionMs = globalPositionMs,
                        playWhenReady = playWhenReady,
                        forceRefresh = true,
                    )
                } else {
                    seekInternal(mediaItemIndex, globalPositionMs - item.playbackSourceOffsetMs())
                }
            null ->
                if (forceRefresh) {
                    reprepare(
                        mediaItemIndex = mediaItemIndex,
                        item = item,
                        protocol = null,
                        globalPositionMs = globalPositionMs,
                        playWhenReady = playWhenReady,
                        forceRefresh = true,
                    )
                } else {
                    seekInternal(mediaItemIndex, globalPositionMs)
                }
        }
    }

    private fun seekInternal(mediaItemIndex: Int, positionMs: Long): Boolean {
        if (positionMs < 0) return false
        internalPlayerOperation = true
        return try {
            player.seekTo(mediaItemIndex, positionMs)
            true
        } finally {
            internalPlayerOperation = false
        }
    }

    private fun reprepare(
        mediaItemIndex: Int,
        item: MediaItem,
        protocol: PlaybackStreamProtocol?,
        globalPositionMs: Long,
        playWhenReady: Boolean,
        forceRefresh: Boolean,
    ): Boolean {
        val trackId = item.playbackTrackId() ?: return false
        val targetPositionMs = globalPositionMs.coerceAtLeast(0)
        if (forceRefresh) grantRepository.invalidate(trackId)

        val canonicalItem = item
            .buildUpon()
            .setUri(PlaybackMediaUri.forTrack(trackId))
            .build()
        val requestItem = when (protocol) {
            PlaybackStreamProtocol.HLS -> canonicalItem.withPlaybackResolution(
                protocol = PlaybackStreamProtocol.HLS,
                sourceOffsetMs = 0,
                requestedStartPositionMs = targetPositionMs.takeIf { it > 0 },
            )
            PlaybackStreamProtocol.PROGRESSIVE -> canonicalItem.withPlaybackResolution(
                protocol = PlaybackStreamProtocol.PROGRESSIVE,
                sourceOffsetMs = 0,
            )
            null -> canonicalItem.withoutPlaybackResolution()
        }
        val localStartPositionMs = if (protocol == PlaybackStreamProtocol.HLS) {
            0
        } else {
            targetPositionMs
        }
        internalPlayerOperation = true
        return try {
            player.stop()
            player.replaceMediaItem(mediaItemIndex, requestItem)
            player.seekTo(mediaItemIndex, localStartPositionMs)
            player.prepare()
            player.playWhenReady = playWhenReady
            true
        } finally {
            internalPlayerOperation = false
        }
    }
}
