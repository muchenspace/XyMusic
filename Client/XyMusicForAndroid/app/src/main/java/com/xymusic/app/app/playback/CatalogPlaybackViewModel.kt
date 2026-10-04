package com.xymusic.app.app.playback

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.xymusic.app.core.ui.media.CatalogTrackUi
import com.xymusic.app.core.ui.media.toPlayerQueueItem
import com.xymusic.app.feature.player.domain.PlayerUseCases
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.launch

@HiltViewModel
class CatalogPlaybackViewModel
@Inject
constructor(private val playerUseCases: PlayerUseCases) : ViewModel() {
    fun playNow(track: CatalogTrackUi) {
        playQueue(tracks = listOf(track), startTrack = track)
    }

    fun playQueue(tracks: List<CatalogTrackUi>, startTrack: CatalogTrackUi, startPositionMs: Long = 0L) {
        val queueItems = tracks.map(CatalogTrackUi::toPlayerQueueItem)
        val startQueueItemId =
            queueItems
                .firstOrNull { queueItem -> queueItem.trackId == startTrack.id }
                ?.queueItemId
                ?: return
        viewModelScope.launch {
            playerUseCases.setQueue(
                items = queueItems,
                startQueueItemId = startQueueItemId,
                startPositionMs = startPositionMs.coerceAtLeast(0L),
                playWhenReady = true,
            )
        }
    }

    fun addToQueue(track: CatalogTrackUi) {
        viewModelScope.launch {
            playerUseCases.addToQueue(listOf(track.toPlayerQueueItem()))
        }
    }
}
