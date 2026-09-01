package com.xymusic.app.feature.player.service

import android.os.SystemClock
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import com.xymusic.app.feature.player.domain.AutomaticPlaybackQualityPolicy
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import com.xymusic.app.feature.player.adapter.media3.globalPlaybackPositionMs

internal class AutomaticQualityPlaybackController(
    private val player: Player,
    private val grantRepository: PlaybackGrantRepository,
    private val qualityController: AutomaticPlaybackQualityPolicy,
    private val elapsedRealtime: () -> Long = SystemClock::elapsedRealtime,
    private val mediaReloadCoordinator: PlaybackMediaReloadCoordinator? = null,
) : Player.Listener {
    private var hasPlayedCurrentItem = false
    private var qualityReloadInProgress = false
    private var suppressRebufferUntilMs = 0L

    override fun onMediaItemTransition(mediaItem: MediaItem?, reason: Int) {
        hasPlayedCurrentItem = false
        qualityReloadInProgress = false
        val trackId = mediaItem?.playbackTrackId()
        if (trackId == null) {
            qualityController.finishActiveTrack()
        } else {
            qualityController.onTrackStarted(
                trackId,
                repeated = reason == Player.MEDIA_ITEM_TRANSITION_REASON_REPEAT,
            )
        }
    }

    override fun onIsPlayingChanged(isPlaying: Boolean) {
        if (!isPlaying) return
        hasPlayedCurrentItem = true
        qualityReloadInProgress = false
        currentTrackId()?.let(qualityController::onTrackStarted)
    }

    override fun onPlaybackStateChanged(playbackState: Int) {
        if (playbackState == Player.STATE_ENDED) {
            qualityController.finishActiveTrack()
            hasPlayedCurrentItem = false
            qualityReloadInProgress = false
            return
        }
        if (playbackState == Player.STATE_READY && player.playWhenReady) {
            hasPlayedCurrentItem = true
            qualityReloadInProgress = false
            currentTrackId()?.let(qualityController::onTrackStarted)
            return
        }
        if (playbackState != Player.STATE_BUFFERING || !player.playWhenReady) return
        if (!hasPlayedCurrentItem || qualityReloadInProgress) return
        if (player.currentPosition < MIN_REBUFFER_POSITION_MS || elapsedRealtime() < suppressRebufferUntilMs) return
        downgradeCurrentTrack()
    }

    override fun onPositionDiscontinuity(
        oldPosition: Player.PositionInfo,
        newPosition: Player.PositionInfo,
        reason: Int,
    ) {
        if (reason == Player.DISCONTINUITY_REASON_SEEK) {
            suppressRebufferUntilMs = elapsedRealtime() + SEEK_REBUFFER_SUPPRESSION_MS
        }
    }

    fun resetForAccountChange() {
        hasPlayedCurrentItem = false
        qualityReloadInProgress = false
        suppressRebufferUntilMs = 0
        qualityController.resetSession()
    }

    private fun downgradeCurrentTrack() {
        val mediaItemIndex = player.currentMediaItemIndex.takeIf { it in 0 until player.mediaItemCount } ?: return
        val trackId = player.getMediaItemAt(mediaItemIndex).playbackTrackId() ?: return
        if (qualityController.onRebuffer(trackId, elapsedRealtime()) == null) return
        val positionMs = player.currentMediaItem?.globalPlaybackPositionMs(player.currentPosition) ?: return
        val resumePlayback = player.playWhenReady
        qualityReloadInProgress = true
        suppressRebufferUntilMs = elapsedRealtime() + QUALITY_SWITCH_REBUFFER_SUPPRESSION_MS
        mediaReloadCoordinator?.reloadCurrent(
            globalPositionMs = positionMs,
            forceRefresh = true,
            playWhenReady = resumePlayback,
        ) ?: run {
            grantRepository.invalidate(trackId)
            player.stop()
            player.seekTo(mediaItemIndex, positionMs)
            player.prepare()
            player.playWhenReady = resumePlayback
        }
    }

    private fun currentTrackId(): String? = player.currentMediaItem?.playbackTrackId()

    private companion object {
        const val SEEK_REBUFFER_SUPPRESSION_MS = 1_500L
        const val QUALITY_SWITCH_REBUFFER_SUPPRESSION_MS = 3_000L
        const val MIN_REBUFFER_POSITION_MS = 3_000L
    }
}
