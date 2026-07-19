package com.xymusic.app.app.playback

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.xymusic.app.core.ui.media.CatalogArtistLinkUi
import com.xymusic.app.core.ui.media.CatalogTrackUi
import com.xymusic.app.core.ui.media.artistNames
import com.xymusic.app.feature.player.domain.PlayerUseCases
import com.xymusic.app.feature.player.domain.model.PlayerQueueItem
import dagger.hilt.android.lifecycle.HiltViewModel
import java.util.UUID
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

private fun CatalogTrackUi.toPlayerQueueItem(): PlayerQueueItem = PlayerQueueItem(
    queueItemId = UUID.randomUUID().toString(),
    trackId = id,
    title = title,
    artistNames = artists.map(CatalogArtistLinkUi::name),
    albumTitle = album?.title,
    artworkUrl = artwork?.url,
    artworkCacheKey = artwork?.cacheKey,
    durationMs = durationMs,
)
