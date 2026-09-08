package com.xymusic.app.feature.player.service

import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import com.xymusic.app.feature.player.adapter.media3.PlaybackMediaUri
import com.xymusic.app.feature.player.adapter.media3.playbackTrackId
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/**
 * Coordinates media reloading (e.g. upon grant expiration) and seeking.
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
            seekInternal(index, globalPositionMs)
        }

    fun reloadCurrent(
        globalPositionMs: Long,
        forceRefresh: Boolean,
        playWhenReady: Boolean = player.playWhenReady,
    ): Job {
        reloadJob?.cancel()
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

    private fun indexOf(queueItemId: String): Int =
        (0 until player.mediaItemCount).firstOrNull { player.getMediaItemAt(it).mediaId == queueItemId } ?: -1

    private fun reloadOrSeek(
        mediaItemIndex: Int,
        globalPositionMs: Long,
        forceRefresh: Boolean,
        playWhenReady: Boolean,
    ): Boolean {
        val item = player.getMediaItemAt(mediaItemIndex)
        return if (forceRefresh) {
            reprepare(
                mediaItemIndex = mediaItemIndex,
                item = item,
                globalPositionMs = globalPositionMs,
                playWhenReady = playWhenReady,
            )
        } else {
            seekInternal(mediaItemIndex, globalPositionMs)
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
        globalPositionMs: Long,
        playWhenReady: Boolean,
    ): Boolean {
        val trackId = item.playbackTrackId() ?: return false
        grantRepository.invalidate(trackId)

        val canonicalItem = item
            .buildUpon()
            .setUri(PlaybackMediaUri.forTrack(trackId))
            .build()
        internalPlayerOperation = true
        return try {
            player.replaceMediaItem(mediaItemIndex, canonicalItem)
            player.seekTo(mediaItemIndex, globalPositionMs.coerceAtLeast(0))
            player.prepare()
            player.playWhenReady = playWhenReady
            true
        } finally {
            internalPlayerOperation = false
        }
    }
}
