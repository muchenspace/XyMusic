package com.xymusic.app.core.model.player

data class PlayerQueueItem(
    val queueItemId: String,
    val trackId: String,
    val title: String,
    val artistNames: List<String>,
    val albumTitle: String?,
    val artworkUrl: String?,
    val artworkCacheKey: String?,
    val durationMs: Long,
) {
    // Evaluated once per instance and excluded from equals(), so queue rows do
    // not rebuild the joined artist line on every recomposition.
    val artistLine: String = artistNames.joinToString(" / ")
}
