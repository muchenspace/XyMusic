package com.xymusic.app.feature.catalog.presentation

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.MoreHoriz
import androidx.compose.material.icons.outlined.Album
import androidx.compose.material.icons.outlined.MusicNote
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.PrimaryTabRow
import androidx.compose.material3.Tab
import androidx.compose.material3.Text
import androidx.compose.material3.VerticalDivider
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.paging.PagingData
import androidx.paging.compose.LazyPagingItems
import androidx.paging.compose.collectAsLazyPagingItems
import com.xymusic.app.R
import com.xymusic.app.core.ui.component.MediaArtwork
import com.xymusic.app.core.ui.media.CachedCatalogBanner
import com.xymusic.app.core.ui.media.CatalogAlbumRow
import com.xymusic.app.core.ui.media.CatalogAlbumUi
import com.xymusic.app.core.ui.media.CatalogArtistLinks
import com.xymusic.app.core.ui.media.CatalogArtwork
import com.xymusic.app.core.ui.media.CatalogPagedList
import com.xymusic.app.core.ui.media.CatalogTrackRow
import com.xymusic.app.core.ui.media.CatalogTrackUi
import com.xymusic.app.core.ui.media.artistNames
import kotlinx.coroutines.flow.Flow

internal object CatalogDetailTestTags {
    const val AlbumLandscapeInfo = "catalog-album-landscape-info"
    const val ArtistLandscapeInfo = "catalog-artist-landscape-info"
    const val LandscapeContent = "catalog-landscape-content"

    fun track(trackId: String): String = "catalog-landscape-track-$trackId"

    fun album(albumId: String): String = "catalog-landscape-album-$albumId"
}

@Composable
internal fun AlbumLandscapeDetail(
    detail: CatalogAlbumDetailUi,
    tracks: LazyPagingItems<CatalogTrackUi>,
    isRefreshing: Boolean,
    refreshFailed: Boolean,
    compactLandscape: Boolean,
    viewportHeight: Dp,
    onArtistClick: (String) -> Unit,
    onTrackMore: (String) -> Unit,
    modifier: Modifier = Modifier,
    onTrackPlay: ((List<CatalogTrackUi>, CatalogTrackUi) -> Unit)? = null,
) {
    val loadedTracks = tracks.itemSnapshotList.items
    Column(modifier = modifier) {
        if (isRefreshing) {
            LinearProgressIndicator(
                modifier = Modifier.fillMaxWidth(),
                color = MaterialTheme.colorScheme.primary,
            )
        }
        if (refreshFailed) CachedCatalogBanner()
        Row(modifier = Modifier.fillMaxSize()) {
            AlbumLandscapeInfoPane(
                detail = detail,
                viewportHeight = viewportHeight,
                onArtistClick = onArtistClick,
                modifier =
                Modifier
                    .weight(0.4f)
                    .fillMaxHeight()
                    .testTag(CatalogDetailTestTags.AlbumLandscapeInfo),
            )
            VerticalDivider(modifier = Modifier.fillMaxHeight())
            CatalogPagedList(
                items = tracks,
                emptyTitle = stringResource(R.string.catalog_tracks_empty_title),
                emptyMessage = stringResource(R.string.catalog_tracks_empty_message),
                emptyIcon = Icons.Outlined.MusicNote,
                itemKey = CatalogTrackUi::id,
                itemContent = { track ->
                    if (compactLandscape) {
                        LandscapeCatalogTrackRow(
                            track = track,
                            onClick = { onTrackPlay?.invoke(loadedTracks, track) },
                            showTrackNumber = true,
                            onPlayClick =
                            onTrackPlay?.let { play ->
                                { play(loadedTracks, track) }
                            },
                            onMoreClick = { onTrackMore(track.id) },
                            modifier = Modifier.testTag(CatalogDetailTestTags.track(track.id)),
                        )
                    } else {
                        CatalogTrackRow(
                            track = track,
                            onClick = { onTrackPlay?.invoke(loadedTracks, track) },
                            showTrackNumber = true,
                            onPlayClick =
                            onTrackPlay?.let { play ->
                                { play(loadedTracks, track) }
                            },
                            onMoreClick = { onTrackMore(track.id) },
                            modifier = Modifier.testTag(CatalogDetailTestTags.track(track.id)),
                        )
                    }
                },
                modifier =
                Modifier
                    .weight(0.6f)
                    .fillMaxHeight()
                    .testTag(CatalogDetailTestTags.LandscapeContent),
            )
        }
    }
}

@Composable
internal fun ArtistLandscapeDetail(
    detail: CatalogArtistDetailUi,
    selectedTab: ArtistDetailTab,
    onTabSelected: (ArtistDetailTab) -> Unit,
    isRefreshing: Boolean,
    refreshFailed: Boolean,
    viewportHeight: Dp,
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    Column(modifier = modifier) {
        if (isRefreshing) {
            LinearProgressIndicator(
                modifier = Modifier.fillMaxWidth(),
                color = MaterialTheme.colorScheme.primary,
            )
        }
        if (refreshFailed) CachedCatalogBanner()
        Row(modifier = Modifier.fillMaxSize()) {
            ArtistLandscapeInfoPane(
                detail = detail,
                viewportHeight = viewportHeight,
                modifier =
                Modifier
                    .weight(0.4f)
                    .fillMaxHeight()
                    .testTag(CatalogDetailTestTags.ArtistLandscapeInfo),
            )
            VerticalDivider(modifier = Modifier.fillMaxHeight())
            Column(
                modifier =
                Modifier
                    .weight(0.6f)
                    .fillMaxHeight()
                    .testTag(CatalogDetailTestTags.LandscapeContent),
            ) {
                ArtistLandscapeTabs(
                    selectedTab = selectedTab,
                    onTabSelected = onTabSelected,
                )
                content()
            }
        }
    }
}

@Composable
internal fun ArtistLandscapeCollection(
    selectedTab: ArtistDetailTab,
    albums: Flow<PagingData<CatalogAlbumUi>>,
    tracks: Flow<PagingData<CatalogTrackUi>>,
    compactLandscape: Boolean,
    onAlbumClick: (String) -> Unit,
    onTrackMore: (String) -> Unit,
    onTrackPlay: ((List<CatalogTrackUi>, CatalogTrackUi) -> Unit)?,
) {
    when (selectedTab) {
        ArtistDetailTab.Albums ->
            ArtistLandscapeAlbums(
                albums = albums.collectAsLazyPagingItems(),
                compactLandscape = compactLandscape,
                onAlbumClick = onAlbumClick,
            )

        ArtistDetailTab.Tracks ->
            ArtistLandscapeTracks(
                tracks = tracks.collectAsLazyPagingItems(),
                compactLandscape = compactLandscape,
                onTrackMore = onTrackMore,
                onTrackPlay = onTrackPlay,
            )
    }
}

@Composable
private fun ArtistLandscapeAlbums(
    albums: LazyPagingItems<CatalogAlbumUi>,
    compactLandscape: Boolean,
    onAlbumClick: (String) -> Unit,
) {
    CatalogPagedList(
        items = albums,
        emptyTitle = stringResource(R.string.catalog_albums_empty_title),
        emptyMessage = stringResource(R.string.catalog_albums_empty_message),
        emptyIcon = Icons.Outlined.Album,
        itemKey = CatalogAlbumUi::id,
        itemContent = { album ->
            if (compactLandscape) {
                LandscapeCatalogAlbumRow(
                    album = album,
                    onClick = { onAlbumClick(album.id) },
                    modifier = Modifier.testTag(CatalogDetailTestTags.album(album.id)),
                )
            } else {
                CatalogAlbumRow(
                    album = album,
                    onClick = { onAlbumClick(album.id) },
                    modifier = Modifier.testTag(CatalogDetailTestTags.album(album.id)),
                )
            }
        },
        modifier = Modifier.fillMaxSize(),
    )
}

@Composable
private fun ArtistLandscapeTracks(
    tracks: LazyPagingItems<CatalogTrackUi>,
    compactLandscape: Boolean,
    onTrackMore: (String) -> Unit,
    onTrackPlay: ((List<CatalogTrackUi>, CatalogTrackUi) -> Unit)?,
) {
    val loadedTracks = tracks.itemSnapshotList.items
    CatalogPagedList(
        items = tracks,
        emptyTitle = stringResource(R.string.catalog_tracks_empty_title),
        emptyMessage = stringResource(R.string.catalog_tracks_empty_message),
        emptyIcon = Icons.Outlined.MusicNote,
        itemKey = CatalogTrackUi::id,
        itemContent = { track ->
            val rowModifier = Modifier.testTag(CatalogDetailTestTags.track(track.id))
            if (compactLandscape) {
                LandscapeCatalogTrackRow(
                    track = track,
                    onClick = { onTrackPlay?.invoke(loadedTracks, track) },
                    onPlayClick =
                    onTrackPlay?.let { play ->
                        { play(loadedTracks, track) }
                    },
                    onMoreClick = { onTrackMore(track.id) },
                    modifier = rowModifier,
                )
            } else {
                CatalogTrackRow(
                    track = track,
                    onClick = { onTrackPlay?.invoke(loadedTracks, track) },
                    onPlayClick =
                    onTrackPlay?.let { play ->
                        { play(loadedTracks, track) }
                    },
                    onMoreClick = { onTrackMore(track.id) },
                    modifier = rowModifier,
                )
            }
        },
        modifier = Modifier.fillMaxSize(),
    )
}

@Composable
private fun AlbumLandscapeInfoPane(
    detail: CatalogAlbumDetailUi,
    viewportHeight: Dp,
    onArtistClick: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val album = detail.album
    val shortViewport = viewportHeight < 360.dp
    val artworkSize = if (shortViewport) 104.dp else 148.dp
    Column(
        modifier =
        modifier
            .verticalScroll(rememberScrollState())
            .padding(if (shortViewport) 10.dp else 16.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(if (shortViewport) 4.dp else 7.dp),
    ) {
        MediaArtwork(
            url = album.cover?.url,
            cacheKey = album.cover?.cacheKey,
            contentDescription = null,
            fallbackIcon = Icons.Outlined.Album,
            modifier = Modifier.size(artworkSize),
            elevation = 6.dp,
        )
        Text(
            text = album.title,
            modifier = Modifier.fillMaxWidth(),
            maxLines = if (shortViewport) 1 else 2,
            overflow = TextOverflow.Ellipsis,
            textAlign = TextAlign.Center,
            style = MaterialTheme.typography.titleLarge,
            fontWeight = FontWeight.Bold,
        )
        CatalogArtistLinks(
            artists = album.artists,
            onArtistClick = onArtistClick,
        )
        Text(
            text =
            listOfNotNull(
                album.releaseDate,
                stringResource(R.string.catalog_track_count, album.trackCount),
            ).joinToString(" \u00b7 "),
            modifier = Modifier.fillMaxWidth(),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.bodySmall,
            textAlign = TextAlign.Center,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            text =
            detail.description?.takeIf(String::isNotBlank)
                ?: stringResource(R.string.catalog_no_description),
            modifier = Modifier.fillMaxWidth(),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.bodySmall,
            textAlign = TextAlign.Center,
            maxLines = if (shortViewport) 2 else 3,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

@Composable
private fun ArtistLandscapeInfoPane(detail: CatalogArtistDetailUi, viewportHeight: Dp, modifier: Modifier = Modifier) {
    val shortViewport = viewportHeight < 360.dp
    val artworkSize = if (shortViewport) 112.dp else 160.dp
    Column(
        modifier =
        modifier
            .verticalScroll(rememberScrollState())
            .padding(if (shortViewport) 10.dp else 18.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(if (shortViewport) 6.dp else 10.dp),
    ) {
        MediaArtwork(
            url = detail.artist.artwork?.url,
            cacheKey = detail.artist.artwork?.cacheKey,
            contentDescription = null,
            fallbackIcon = Icons.Outlined.Person,
            shape = CircleShape,
            modifier = Modifier.size(artworkSize),
            elevation = 6.dp,
        )
        Text(
            text = detail.artist.name,
            modifier = Modifier.fillMaxWidth(),
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            textAlign = TextAlign.Center,
            style = MaterialTheme.typography.titleLarge,
            fontWeight = FontWeight.Bold,
        )
        Text(
            text =
            detail.description?.takeIf(String::isNotBlank)
                ?: stringResource(R.string.catalog_no_description),
            modifier = Modifier.fillMaxWidth(),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.bodySmall,
            textAlign = TextAlign.Center,
            maxLines = if (shortViewport) 2 else 4,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

@Composable
private fun ArtistLandscapeTabs(selectedTab: ArtistDetailTab, onTabSelected: (ArtistDetailTab) -> Unit) {
    PrimaryTabRow(
        selectedTabIndex = selectedTab.ordinal,
        containerColor = Color.Transparent,
        contentColor = MaterialTheme.colorScheme.onSurface,
        divider = { HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant) },
    ) {
        ArtistDetailTab.entries.forEach { tab ->
            Tab(
                selected = selectedTab == tab,
                onClick = { onTabSelected(tab) },
                text = {
                    Text(
                        stringResource(
                            if (tab == ArtistDetailTab.Albums) {
                                R.string.catalog_artist_albums
                            } else {
                                R.string.catalog_artist_tracks
                            },
                        ),
                        style = MaterialTheme.typography.labelMedium,
                    )
                },
            )
        }
    }
}

@Composable
internal fun LandscapeCatalogTrackRow(
    track: CatalogTrackUi,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    showTrackNumber: Boolean = false,
    onPlayClick: (() -> Unit)? = null,
    onMoreClick: (() -> Unit)? = null,
) {
    val playDescription = stringResource(R.string.player_play_track, track.title)
    val artistLine = track.artistNames().ifBlank { stringResource(R.string.catalog_unknown_artist) }
    val rowModifier =
        if (onPlayClick != null) {
            modifier.semantics { contentDescription = playDescription }
        } else {
            modifier
        }
    Column(modifier = Modifier.fillMaxWidth()) {
        Row(
            modifier =
            rowModifier
                .fillMaxWidth()
                .clickable(
                    role = Role.Button,
                    onClick = onPlayClick ?: onClick,
                )
                .padding(start = 14.dp, end = 6.dp, top = 4.dp, bottom = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (!showTrackNumber) {
                CatalogArtwork(
                    artwork = track.artwork,
                    fallbackIcon = Icons.Outlined.MusicNote,
                    size = 40.dp,
                    shape = RoundedCornerShape(5.dp),
                )
                Spacer(modifier = Modifier.width(10.dp))
            }
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = landscapeTrackTitle(track, showTrackNumber),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Text(
                    text =
                    buildString {
                        append(artistLine)
                        track.album?.title?.takeIf(String::isNotBlank)?.let { album ->
                            append(" \u00b7 ").append(album)
                        }
                    },
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.bodySmall,
                )
            }
            onMoreClick?.let { more ->
                IconButton(onClick = more, modifier = Modifier.size(40.dp)) {
                    Icon(
                        imageVector = Icons.Default.MoreHoriz,
                        contentDescription = stringResource(R.string.common_more_actions),
                        tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
        HorizontalDivider(
            modifier = Modifier.padding(start = if (showTrackNumber) 14.dp else 64.dp),
            thickness = 0.5.dp,
            color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.65f),
        )
    }
}

@Composable
internal fun LandscapeCatalogAlbumRow(album: CatalogAlbumUi, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Column(modifier = Modifier.fillMaxWidth()) {
        Row(
            modifier =
            modifier
                .fillMaxWidth()
                .clickable(
                    role = Role.Button,
                    onClick = onClick,
                )
                .padding(horizontal = 14.dp, vertical = 5.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            CatalogArtwork(
                artwork = album.cover,
                fallbackIcon = Icons.Outlined.Album,
                size = 46.dp,
                shape = RoundedCornerShape(6.dp),
            )
            Spacer(modifier = Modifier.width(10.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = album.title,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    style = MaterialTheme.typography.bodyMedium,
                    fontWeight = FontWeight.Medium,
                )
                Text(
                    text = album.artistNames().ifBlank { stringResource(R.string.catalog_unknown_artist) },
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    color = MaterialTheme.colorScheme.primary,
                    style = MaterialTheme.typography.bodySmall,
                )
                Text(
                    text =
                    listOfNotNull(
                        album.releaseDate,
                        stringResource(R.string.catalog_track_count, album.trackCount),
                    ).joinToString(" \u00b7 "),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.labelSmall,
                )
            }
        }
        HorizontalDivider(
            modifier = Modifier.padding(start = 70.dp),
            thickness = 0.5.dp,
            color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.65f),
        )
    }
}

private fun landscapeTrackTitle(track: CatalogTrackUi, showTrackNumber: Boolean): String {
    if (!showTrackNumber) return track.title
    val number = track.trackNumber ?: return track.title
    val order = if (track.discNumber > 1) "${track.discNumber}-$number" else number.toString()
    return "$order  ${track.title}"
}
