package com.xymusic.app.feature.player.service

import android.os.SystemClock
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.HttpDataSource
import com.xymusic.app.feature.player.adapter.media3.globalPlaybackPositionMs
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository

/**
 * Reopens a source after a server playback grant has expired while the player
 * was paused or buffered. Media3 may keep the old DataSource alive across a
 * pause, so refreshing grants only in DataSource.open is not sufficient.
 */
@UnstableApi
internal class PlaybackGrantRecoveryController(
    private val player: Player,
    private val grantRepository: PlaybackGrantRepository,
    private val mediaReloadCoordinator: PlaybackMediaReloadCoordinator? = null,
) : Player.Listener {
    private var lastRecoveredMediaId: String? = null
    private var lastRecoveryAtElapsedRealtimeMs = 0L
    private var lastPlayWhenReady = player.playWhenReady

    override fun onPlayerError(error: PlaybackException) {
        if (!isExpiredPlaybackGrantError(error)) return
        val mediaItemIndex =
            player.currentMediaItemIndex.takeIf { index -> index in 0 until player.mediaItemCount }
                ?: return
        val mediaItem = player.getMediaItemAt(mediaItemIndex)
        val mediaId = mediaItem.mediaId.takeIf(String::isNotBlank) ?: return
        val now = SystemClock.elapsedRealtime()
        if (
            lastRecoveredMediaId == mediaId &&
                now - lastRecoveryAtElapsedRealtimeMs < RECOVERY_RETRY_COOLDOWN_MS
        ) {
            return
        }
        val trackId = mediaItem.playbackTrackId() ?: return
        val shouldPlay = player.playWhenReady || lastPlayWhenReady
        lastRecoveredMediaId = mediaId
        lastRecoveryAtElapsedRealtimeMs = now
        val positionMs = mediaItem.globalPlaybackPositionMs(player.currentPosition)
        mediaReloadCoordinator?.reloadCurrent(
            globalPositionMs = positionMs,
            forceRefresh = true,
            playWhenReady = shouldPlay,
        ) ?: run {
            grantRepository.invalidate(trackId)
            player.seekTo(mediaItemIndex, player.currentPosition.coerceAtLeast(0))
            player.prepare()
            player.playWhenReady = shouldPlay
        }
    }

    override fun onPlayWhenReadyChanged(playWhenReady: Boolean, reason: Int) {
        lastPlayWhenReady = playWhenReady
    }

    override fun onIsPlayingChanged(isPlaying: Boolean) {
        if (isPlaying) {
            // A successful recovery may be needed again after the next long
            // pause, so a new playing transition starts a fresh recovery window.
            lastRecoveredMediaId = null
            lastRecoveryAtElapsedRealtimeMs = 0L
        }
    }

    override fun onMediaItemTransition(mediaItem: MediaItem?, reason: Int) {
        if (mediaItem?.mediaId != lastRecoveredMediaId) {
            lastRecoveredMediaId = null
            lastRecoveryAtElapsedRealtimeMs = 0L
        }
    }

    fun resetForAccountChange() {
        lastRecoveredMediaId = null
        lastRecoveryAtElapsedRealtimeMs = 0L
        lastPlayWhenReady = player.playWhenReady
    }

    private companion object {
        const val RECOVERY_RETRY_COOLDOWN_MS = 10_000L
    }
}

@UnstableApi
internal fun isExpiredPlaybackGrantError(error: PlaybackException): Boolean {
    // Only explicit authorization/resolution responses signal an expired (or
    // invalidated) grant. A generic HTTP status failure must NOT be treated as
    // expiry: a 5xx from a server transcode failure would then force a grant
    // reload loop instead of surfacing the real playback error.
    return generateSequence<Throwable>(error) { it.cause }
        .filterIsInstance<HttpDataSource.InvalidResponseCodeException>()
        .any { it.responseCode == 401 || it.responseCode == 403 || it.responseCode == 404 || it.responseCode == 410 }
}
