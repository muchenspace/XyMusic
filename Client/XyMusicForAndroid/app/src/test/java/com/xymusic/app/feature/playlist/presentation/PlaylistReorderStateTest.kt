package com.xymusic.app.feature.playlist.presentation

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.ui.media.CatalogTrackUi
import org.junit.Test

class PlaylistReorderStateTest {
    @Test
    fun moveKeepsVisibleOrderBackedByDescendingPositions() {
        val state =
            PlaylistReorderState(
                listOf(
                    entry("newest", 2),
                    entry("middle", 1),
                    entry("oldest", 0),
                ),
            )

        assertThat(state.move("oldest", -1)).isTrue()

        assertThat(state.entries.map(PlaylistEntryUi::entryId))
            .containsExactly("newest", "oldest", "middle")
            .inOrder()
        assertThat(state.entries.map(PlaylistEntryUi::position))
            .containsExactly(2, 1, 0)
            .inOrder()
        assertThat(state.finish())
            .containsExactly("newest", "oldest", "middle")
            .inOrder()
    }

    @Test
    fun dragLifecycleExposesActiveEntryAndCancelRestoresSourceOrder() {
        val original =
            listOf(
                entry("first", 2),
                entry("second", 1),
                entry("third", 0),
            )
        val state = PlaylistReorderState(original)

        assertThat(state.startDrag("second")).isTrue()
        assertThat(state.draggedEntryId).isEqualTo("second")
        assertThat(state.move("second", 1)).isTrue()
        assertThat(state.entries.map(PlaylistEntryUi::entryId))
            .containsExactly("first", "third", "second")
            .inOrder()

        state.cancel()

        assertThat(state.draggedEntryId).isNull()
        assertThat(state.entries).containsExactlyElementsIn(original).inOrder()
    }

    private fun entry(id: String, position: Int) = PlaylistEntryUi(
        entryId = id,
        position = position,
        track =
        CatalogTrackUi(
            id = "track-$id",
            title = id,
            artists = emptyList(),
            album = null,
            artwork = null,
            durationMs = 1_000,
            discNumber = 1,
            trackNumber = null,
        ),
    )
}
