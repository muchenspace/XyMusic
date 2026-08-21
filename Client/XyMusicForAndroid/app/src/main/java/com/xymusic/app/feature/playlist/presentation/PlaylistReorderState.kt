package com.xymusic.app.feature.playlist.presentation

import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue

@Stable
internal class PlaylistReorderState(initialEntries: List<PlaylistEntryUi> = emptyList()) {
    var entries: List<PlaylistEntryUi> by mutableStateOf(initialEntries)
        private set

    var draggedEntryId: String? by mutableStateOf(null)
        private set

    private var sourceEntries: List<PlaylistEntryUi> = initialEntries
    private var reordering = false

    fun sync(entries: List<PlaylistEntryUi>) {
        sourceEntries = entries
        if (!reordering) this.entries = entries
    }

    fun startDrag(entryId: String): Boolean {
        if (entries.none { it.entryId == entryId }) return false
        reordering = true
        draggedEntryId = entryId
        return true
    }

    fun move(entryId: String, direction: Int): Boolean {
        val oldIndex = entries.indexOfFirst { it.entryId == entryId }
        val newIndex = oldIndex + direction
        if (oldIndex < 0 || newIndex !in entries.indices) return false
        reordering = true
        val lastPosition = entries.lastIndex
        entries =
            entries
                .toMutableList()
                .apply {
                    add(newIndex, removeAt(oldIndex))
                }.mapIndexed { index, entry -> entry.copy(position = lastPosition - index) }
        return true
    }

    fun finish(): List<String>? {
        reordering = false
        draggedEntryId = null
        val orderedIds = entries.map(PlaylistEntryUi::entryId)
        return orderedIds.takeIf { it != sourceEntries.map(PlaylistEntryUi::entryId) }
    }

    fun cancel() {
        reordering = false
        draggedEntryId = null
        entries = sourceEntries
    }
}
