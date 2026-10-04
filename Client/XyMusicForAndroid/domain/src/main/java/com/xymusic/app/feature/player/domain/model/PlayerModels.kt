package com.xymusic.app.feature.player.domain.model

typealias PlayerQueueItem = com.xymusic.app.core.model.player.PlayerQueueItem

data class PlayerState(
    val connectionState: PlayerConnectionState = PlayerConnectionState.DISCONNECTED,
    val playbackState: PlaybackState = PlaybackState.IDLE,
    val queue: List<PlayerQueueItem> = emptyList(),
    val currentQueueItemId: String? = null,
    val isPlaying: Boolean = false,
    val positionMs: Long = 0,
    val positionAnchorElapsedRealtimeMs: Long? = null,
    val positionDiscontinuitySequence: Long = 0,
    val bufferedPositionMs: Long = 0,
    val durationMs: Long = 0,
    val repeatMode: RepeatMode = RepeatMode.ALL,
    val shuffleEnabled: Boolean = false,
    val playbackSpeed: Float = 1f,
    val sleepTimerRemainingMs: Long? = null,
    val failure: PlayerFailure? = null,
) {
    // Resolved once per state instance. Composing call sites read this several
    // times per recomposition, so a computed getter would rescan the whole queue
    // on every read.
    val currentItem: PlayerQueueItem? = queue.firstOrNull { it.queueItemId == currentQueueItemId }
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
    ONE,
    ALL,
}

sealed interface PlayerFailure {
    data object ConnectionUnavailable : PlayerFailure

    data object InvalidQueue : PlayerFailure

    data object PlaybackUnavailable : PlayerFailure

    data class Unexpected(val message: String?) : PlayerFailure
}
