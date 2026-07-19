package com.xymusic.app.feature.player.domain.model

data class PlayerQueueItem(
    val queueItemId: String,
    val trackId: String,
    val title: String,
    val artistNames: List<String>,
    val albumTitle: String?,
    val artworkUrl: String?,
    val artworkCacheKey: String?,
    val durationMs: Long,
)

data class PlayerState(
    val connectionState: PlayerConnectionState = PlayerConnectionState.DISCONNECTED,
    val playbackState: PlaybackState = PlaybackState.IDLE,
    val queue: List<PlayerQueueItem> = emptyList(),
    val currentQueueItemId: String? = null,
    val isPlaying: Boolean = false,
    val positionMs: Long = 0,
    val bufferedPositionMs: Long = 0,
    val durationMs: Long = 0,
    val repeatMode: RepeatMode = RepeatMode.OFF,
    val shuffleEnabled: Boolean = false,
    val playbackSpeed: Float = 1f,
    val sleepTimerRemainingMs: Long? = null,
    val failure: PlayerFailure? = null,
) {
    val currentItem: PlayerQueueItem?
        get() = queue.firstOrNull { it.queueItemId == currentQueueItemId }
}

enum class PlayerConnectionState {
    DISCONNECTED,
    CONNECTING,
    CONNECTED,
}

enum class PlaybackState {
    IDLE,
    BUFFERING,
    READY,
    ENDED,
}

enum class RepeatMode {
    OFF,
    ONE,
    ALL,
}

enum class PreferredQuality {
    AUTO,
    DATA_SAVER,
    STANDARD,
    HIGH,
    LOSSLESS,
}

sealed interface PlayerFailure {
    data object ConnectionUnavailable : PlayerFailure

    data object InvalidQueue : PlayerFailure

    data object PlaybackUnavailable : PlayerFailure

    data class Unexpected(val message: String?) : PlayerFailure
}
