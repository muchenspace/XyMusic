package com.xymusic.app.feature.playlist.domain

import com.xymusic.app.core.network.DomainError
import com.xymusic.app.feature.playlist.domain.model.AddPlaylistTrackCommand
import com.xymusic.app.feature.playlist.domain.model.CreatePlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.PlaylistDetail
import com.xymusic.app.feature.playlist.domain.model.PlaylistDetailPage
import com.xymusic.app.feature.playlist.domain.model.PlaylistSort
import com.xymusic.app.feature.playlist.domain.model.PlaylistSummary
import com.xymusic.app.feature.playlist.domain.model.PlaylistVersionConflict
import com.xymusic.app.feature.playlist.domain.model.RemovePlaylistTrackCommand
import com.xymusic.app.feature.playlist.domain.model.ReorderPlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.UpdatePlaylistCommand
import kotlinx.coroutines.flow.Flow

interface PlaylistRepository {
    fun observePlaylists(): Flow<List<PlaylistSummary>>

    fun observePlaylist(playlistId: String): Flow<PlaylistDetail?>

    suspend fun refreshPlaylists(sort: PlaylistSort = PlaylistSort.UPDATED_DESC): PlaylistResult<Unit>

    suspend fun refreshPlaylist(playlistId: String): PlaylistResult<Unit>

    suspend fun loadPlaylistPage(playlistId: String, cursor: String?): PlaylistResult<PlaylistDetailPage> =
        PlaylistResult.Failure(DomainError.Protocol("Playlist paging is unavailable", null, null))

    suspend fun create(command: CreatePlaylistCommand): PlaylistResult<PlaylistSummary>

    suspend fun update(command: UpdatePlaylistCommand): PlaylistResult<PlaylistSummary>

    suspend fun delete(playlistId: String, expectedVersion: Long): PlaylistResult<Unit>

    suspend fun addTrack(command: AddPlaylistTrackCommand): PlaylistResult<Unit>

    suspend fun removeTrack(command: RemovePlaylistTrackCommand): PlaylistResult<Unit>

    suspend fun reorder(command: ReorderPlaylistCommand): PlaylistResult<Unit>
}

sealed interface PlaylistResult<out T> {
    data class Success<T>(val value: T) : PlaylistResult<T>

    data class Failure(val error: DomainError) : PlaylistResult<Nothing>

    data class Conflict(val conflict: PlaylistVersionConflict) : PlaylistResult<Nothing>
}
