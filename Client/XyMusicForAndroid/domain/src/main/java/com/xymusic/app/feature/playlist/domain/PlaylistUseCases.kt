package com.xymusic.app.feature.playlist.domain

import com.xymusic.app.feature.playlist.domain.model.AddPlaylistTrackCommand
import com.xymusic.app.feature.playlist.domain.model.CreatePlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.PlaylistSort
import com.xymusic.app.feature.playlist.domain.model.RemovePlaylistTrackCommand
import com.xymusic.app.feature.playlist.domain.model.ReorderPlaylistCommand
import com.xymusic.app.feature.playlist.domain.model.UpdatePlaylistCommand
import javax.inject.Inject

class PlaylistUseCases
@Inject
constructor(private val repository: PlaylistRepository) {
    fun playlists() = repository.observePlaylists()

    fun playlist(playlistId: String) = repository.observePlaylist(playlistId)

    suspend fun refreshPlaylists(sort: PlaylistSort = PlaylistSort.UPDATED_DESC) = repository.refreshPlaylists(sort)

    suspend fun refreshPlaylist(playlistId: String) = repository.refreshPlaylist(playlistId)

    suspend fun loadPlaylistPage(playlistId: String, cursor: String? = null) =
        repository.loadPlaylistPage(playlistId, cursor)

    suspend fun create(command: CreatePlaylistCommand) = repository.create(command)

    suspend fun update(command: UpdatePlaylistCommand) = repository.update(command)

    suspend fun delete(playlistId: String, expectedVersion: Long) = repository.delete(playlistId, expectedVersion)

    suspend fun addTrack(command: AddPlaylistTrackCommand) = repository.addTrack(command)

    suspend fun removeTrack(command: RemovePlaylistTrackCommand) = repository.removeTrack(command)

    suspend fun reorder(command: ReorderPlaylistCommand) = repository.reorder(command)
}
