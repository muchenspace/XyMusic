package com.xymusic.app.feature.playlist.data

import com.xymusic.app.core.database.dao.CatalogDao
import com.xymusic.app.core.database.dao.PlaylistDao
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.feature.playlist.domain.model.PlaylistDetail
import com.xymusic.app.feature.playlist.domain.model.PlaylistSummary
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.mapLatest

@OptIn(ExperimentalCoroutinesApi::class)
internal class PlaylistRepositoryQueries(
    private val playlistDao: PlaylistDao,
    private val catalogDao: CatalogDao,
    private val sessionProvider: AppSessionProvider,
) {
    fun observePlaylists(): Flow<List<PlaylistSummary>> = sessionProvider.sessionState.flatMapLatest { state ->
        val owner = (state as? AppSessionState.SignedIn)?.userId
        if (owner == null) {
            flowOf(emptyList())
        } else {
            playlistDao.observePlaylists(owner).mapLatest { rows -> rows.map { it.toDomain() } }
        }
    }

    fun observePlaylist(playlistId: String): Flow<PlaylistDetail?> =
        sessionProvider.sessionState.flatMapLatest { state ->
            val owner = (state as? AppSessionState.SignedIn)?.userId
            if (owner == null) {
                flowOf(null)
            } else {
                playlistDao.observePlaylistEntity(owner, playlistId).mapLatest { playlist ->
                    if (playlist == null) {
                        null
                    } else {
                        playlistDao
                            .snapshot(owner, playlistId)
                            ?.takeIf { snapshot -> snapshot.hasCompleteEntries() }
                            ?.toDomain(catalogDao)
                    }
                }
            }
        }
}
