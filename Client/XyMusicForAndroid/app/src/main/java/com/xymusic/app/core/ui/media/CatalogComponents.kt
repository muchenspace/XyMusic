package com.xymusic.app.core.ui.media

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CloudOff
import androidx.compose.material.icons.filled.MoreHoriz
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.outlined.Album
import androidx.compose.material.icons.outlined.MusicNote
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.paging.LoadState
import androidx.paging.compose.LazyPagingItems
import com.xymusic.app.R
import com.xymusic.app.core.ui.component.EmptyState
import com.xymusic.app.core.ui.component.ErrorState
import com.xymusic.app.core.ui.component.LoadingState
import com.xymusic.app.core.ui.component.MediaArtwork

@Composable
internal fun CatalogTrackRow(
    track: CatalogTrackUi,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    showTrackNumber: Boolean = false,
    onPlayClick: (() -> Unit)? = null,
    onMoreClick: (() -> Unit)? = null,
) {
    val playDescription = stringResource(R.string.player_play_track, track.title)
    val artistLine =
        track.artistNames().ifBlank {
            stringResource(R.string.catalog_unknown_artist)
        }
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
                .padding(start = 20.dp, end = 8.dp, top = 7.dp, bottom = 7.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (!showTrackNumber) {
                CatalogArtwork(
                    artwork = track.artwork,
                    fallbackIcon = Icons.Outlined.MusicNote,
                    size = 50.dp,
                    shape = RoundedCornerShape(6.dp),
                )
                Spacer(modifier = Modifier.width(12.dp))
            }
            Column(
                modifier = Modifier.weight(1f),
                verticalArrangement = Arrangement.spacedBy(1.dp),
            ) {
                Text(
                    text =
                    if (showTrackNumber) {
                        track.orderLabel()?.let { "$it  ${track.title}" } ?: track.title
                    } else {
                        track.title
                    },
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Text(
                    text =
                    buildString {
                        append(artistLine)
                        track.album?.title?.takeIf(String::isNotBlank)?.let { album ->
                            append(" · ").append(album)
                        }
                    },
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.bodyMedium,
                )
            }
            onMoreClick?.let { more ->
                IconButton(onClick = more, modifier = Modifier.size(44.dp)) {
                    Icon(
                        imageVector = Icons.Default.MoreHoriz,
                        contentDescription = stringResource(R.string.common_more_actions),
                        tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
        HorizontalDivider(
            modifier = Modifier.padding(start = if (showTrackNumber) 20.dp else 82.dp),
            thickness = 0.5.dp,
            color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.65f),
        )
    }
}

@Composable
internal fun CatalogAlbumRow(album: CatalogAlbumUi, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Column(modifier = Modifier.fillMaxWidth()) {
        Row(
            modifier =
            modifier
                .fillMaxWidth()
                .clickable(
                    role = Role.Button,
                    onClick = onClick,
                )
                .padding(horizontal = 20.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            CatalogArtwork(
                artwork = album.cover,
                fallbackIcon = Icons.Outlined.Album,
                size = 62.dp,
                shape = RoundedCornerShape(8.dp),
            )
            Spacer(modifier = Modifier.width(12.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = album.title,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    style = MaterialTheme.typography.bodyLarge,
                )
                Text(
                    text =
                    album.artistNames().ifBlank {
                        stringResource(R.string.catalog_unknown_artist)
                    },
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    color = MaterialTheme.colorScheme.primary,
                    style = MaterialTheme.typography.bodyMedium,
                )
                Text(
                    text =
                    listOfNotNull(
                        album.releaseDate,
                        stringResource(R.string.catalog_track_count, album.trackCount),
                    ).joinToString(" · "),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.bodySmall,
                )
            }
        }
        HorizontalDivider(
            modifier = Modifier.padding(start = 94.dp),
            thickness = 0.5.dp,
            color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.65f),
        )
    }
}

@Composable
internal fun CatalogArtistRow(artist: CatalogArtistUi, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Column(modifier = Modifier.fillMaxWidth()) {
        Row(
            modifier =
            modifier
                .fillMaxWidth()
                .clickable(
                    role = Role.Button,
                    onClick = onClick,
                )
                .padding(horizontal = 20.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            CatalogArtwork(
                artwork = artist.artwork,
                fallbackIcon = Icons.Outlined.Person,
                size = 58.dp,
                shape = CircleShape,
            )
            Spacer(modifier = Modifier.width(12.dp))
            Text(
                text = artist.name,
                modifier = Modifier.weight(1f),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                style = MaterialTheme.typography.bodyLarge,
                fontWeight = FontWeight.Medium,
            )
        }
        HorizontalDivider(
            modifier = Modifier.padding(start = 90.dp),
            thickness = 0.5.dp,
            color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.65f),
        )
    }
}

@Composable
internal fun CatalogArtwork(
    artwork: CatalogArtworkUi?,
    fallbackIcon: ImageVector,
    modifier: Modifier = Modifier,
    size: Dp = 52.dp,
    shape: Shape = RoundedCornerShape(8.dp),
) {
    MediaArtwork(
        url = artwork?.url,
        cacheKey = artwork?.cacheKey,
        contentDescription = null,
        fallbackIcon = fallbackIcon,
        shape = shape,
        modifier = modifier.size(size),
    )
}

@Composable
internal fun CatalogAlbumShelfCard(
    album: CatalogAlbumUi,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    width: Dp = 168.dp,
) {
    Column(
        modifier =
        modifier
            .width(width)
            .clickable(role = Role.Button, onClick = onClick),
        verticalArrangement = Arrangement.spacedBy(5.dp),
    ) {
        MediaArtwork(
            url = album.cover?.url,
            cacheKey = album.cover?.cacheKey,
            contentDescription = null,
            fallbackIcon = Icons.Outlined.Album,
            shape = RoundedCornerShape(9.dp),
            modifier = Modifier.fillMaxWidth().aspectRatio(1f),
        )
        Text(
            text = album.title,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            style = MaterialTheme.typography.bodyLarge,
            fontWeight = FontWeight.Medium,
        )
        Text(
            text = album.artistNames().ifBlank { stringResource(R.string.catalog_unknown_artist) },
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.bodyMedium,
        )
    }
}

@Composable
internal fun CatalogArtistShelfCard(
    artist: CatalogArtistUi,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    width: Dp = 140.dp,
) {
    Column(
        modifier =
        modifier
            .width(width)
            .clickable(role = Role.Button, onClick = onClick),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(7.dp),
    ) {
        MediaArtwork(
            url = artist.artwork?.url,
            cacheKey = artist.artwork?.cacheKey,
            contentDescription = null,
            fallbackIcon = Icons.Outlined.Person,
            shape = CircleShape,
            modifier = Modifier.fillMaxWidth().aspectRatio(1f),
        )
        Text(
            text = artist.name,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            style = MaterialTheme.typography.bodyMedium,
            fontWeight = FontWeight.Medium,
        )
    }
}

@Composable
internal fun CatalogTrackShelfCard(
    track: CatalogTrackUi,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    onMoreClick: (() -> Unit)? = null,
    width: Dp = 160.dp,
) {
    Column(
        modifier = modifier.width(width),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Box(
            modifier =
            Modifier
                .fillMaxWidth()
                .aspectRatio(1f)
                .clip(RoundedCornerShape(9.dp))
                .clickable(role = Role.Button, onClick = onClick),
        ) {
            MediaArtwork(
                url = track.artwork?.url,
                cacheKey = track.artwork?.cacheKey,
                contentDescription = null,
                fallbackIcon = Icons.Outlined.MusicNote,
                shape = RoundedCornerShape(9.dp),
                modifier = Modifier.fillMaxSize(),
            )
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = track.title,
                modifier = Modifier.weight(1f),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                style = MaterialTheme.typography.bodyMedium,
                fontWeight = FontWeight.Medium,
            )
            onMoreClick?.let { more ->
                Icon(
                    imageVector = Icons.Default.MoreHoriz,
                    contentDescription = stringResource(R.string.common_more_actions),
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier =
                    Modifier
                        .size(24.dp)
                        .clip(CircleShape)
                        .clickable(onClick = more)
                        .padding(2.dp),
                )
            }
        }
        Text(
            text = track.artistNames().ifBlank { stringResource(R.string.catalog_unknown_artist) },
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.bodySmall,
        )
    }
}

@Composable
internal fun CatalogArtistLinks(
    artists: List<CatalogArtistLinkUi>,
    onArtistClick: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    if (artists.isEmpty()) {
        Text(
            text = stringResource(R.string.catalog_unknown_artist),
            modifier = modifier,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.bodyLarge,
        )
        return
    }
    Row(
        modifier = modifier.horizontalScroll(rememberScrollState()),
        horizontalArrangement = Arrangement.spacedBy(2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        artists.forEachIndexed { index, artist ->
            if (index > 0) Text("·", color = MaterialTheme.colorScheme.primary)
            Text(
                text = artist.name,
                color = MaterialTheme.colorScheme.primary,
                style = MaterialTheme.typography.bodyLarge,
                modifier =
                Modifier
                    .clip(RoundedCornerShape(4.dp))
                    .clickable { onArtistClick(artist.id) }
                    .padding(horizontal = 3.dp, vertical = 2.dp),
            )
        }
    }
}

@Composable
internal fun CachedCatalogBanner(modifier: Modifier = Modifier, onRetry: (() -> Unit)? = null) {
    Row(
        modifier =
        modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.primary.copy(alpha = 0.08f))
            .padding(horizontal = 20.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Icon(
            imageVector = Icons.Default.CloudOff,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.primary,
            modifier = Modifier.size(18.dp),
        )
        Text(
            text = stringResource(R.string.catalog_cached_content),
            modifier = Modifier.weight(1f),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        onRetry?.let { retry ->
            TextButton(onClick = retry) {
                Text(
                    stringResource(R.string.catalog_refresh),
                    color = MaterialTheme.colorScheme.primary,
                )
            }
        }
    }
}

@Composable
internal fun <T : Any> CatalogPagedList(
    items: LazyPagingItems<T>,
    emptyTitle: String,
    emptyMessage: String,
    emptyIcon: ImageVector,
    itemKey: (T) -> Any,
    itemContent: @Composable (T) -> Unit,
    modifier: Modifier = Modifier,
    header: (@Composable () -> Unit)? = null,
    listState: LazyListState = rememberLazyListState(),
) {
    val refresh = items.loadState.refresh
    val append = items.loadState.append
    val hasHeader = header != null

    when {
        items.itemCount == 0 && refresh is LoadState.Loading && !hasHeader -> {
            LoadingState(modifier = modifier)
        }
        items.itemCount == 0 && refresh is LoadState.Error && !hasHeader -> {
            ErrorState(onRetry = items::retry, modifier = modifier.fillMaxSize())
        }
        items.itemCount == 0 && refresh is LoadState.NotLoading && !hasHeader -> {
            Box(modifier = modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                EmptyState(title = emptyTitle, message = emptyMessage, icon = emptyIcon)
            }
        }
        else -> {
            LazyColumn(
                state = listState,
                modifier = modifier.fillMaxSize(),
                contentPadding = PaddingValues(bottom = 32.dp),
            ) {
                if (refresh is LoadState.Loading && items.itemCount > 0) {
                    item(key = "catalog-refresh-loading") {
                        LinearProgressIndicator(
                            modifier = Modifier.fillMaxWidth(),
                            color = MaterialTheme.colorScheme.primary,
                        )
                    }
                }
                if (refresh is LoadState.Error && items.itemCount > 0) {
                    item(key = "catalog-cached-banner") {
                        CachedCatalogBanner(onRetry = items::retry)
                    }
                }
                header?.let { headerContent ->
                    item(key = "catalog-header") { headerContent() }
                }
                if (items.itemCount == 0) {
                    when (refresh) {
                        is LoadState.Loading ->
                            item(key = "catalog-initial-loading") {
                                LoadingState(modifier = Modifier.fillMaxWidth().height(240.dp))
                            }
                        is LoadState.Error ->
                            item(key = "catalog-initial-error") {
                                ErrorState(onRetry = items::retry, modifier = Modifier.fillMaxWidth())
                            }
                        is LoadState.NotLoading ->
                            item(key = "catalog-empty") {
                                EmptyState(title = emptyTitle, message = emptyMessage, icon = emptyIcon)
                            }
                    }
                }
                this.items(
                    count = items.itemCount,
                    key = { index -> items.peek(index)?.let(itemKey) ?: "catalog-loading-$index" },
                ) { index ->
                    val item = items[index]
                    if (item != null) {
                        itemContent(item)
                    }
                }
                when (append) {
                    is LoadState.Loading ->
                        item(key = "catalog-append-loading") {
                            Row(
                                modifier = Modifier.fillMaxWidth().padding(20.dp),
                                horizontalArrangement = Arrangement.Center,
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                CircularProgressIndicator(
                                    modifier = Modifier.size(20.dp),
                                    color = MaterialTheme.colorScheme.primary,
                                    strokeWidth = 2.dp,
                                )
                                Spacer(modifier = Modifier.width(8.dp))
                                Text(stringResource(R.string.catalog_loading_more))
                            }
                        }
                    is LoadState.Error ->
                        item(key = "catalog-append-error") {
                            Column(
                                modifier = Modifier.fillMaxWidth().padding(20.dp),
                                horizontalAlignment = Alignment.CenterHorizontally,
                            ) {
                                Text(
                                    text = stringResource(R.string.catalog_load_more_failed),
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                                TextButton(onClick = items::retry) {
                                    Icon(
                                        Icons.Default.Refresh,
                                        contentDescription = null,
                                        tint = MaterialTheme.colorScheme.primary,
                                    )
                                    Spacer(modifier = Modifier.width(8.dp))
                                    Text(
                                        stringResource(R.string.common_retry),
                                        color = MaterialTheme.colorScheme.primary,
                                    )
                                }
                            }
                        }
                    is LoadState.NotLoading -> Unit
                }
            }
        }
    }
}

@Composable
internal fun formatDuration(durationMs: Long): String {
    val totalSeconds = durationMs.coerceAtLeast(0L) / 1_000L
    return stringResource(
        R.string.catalog_minutes_seconds,
        totalSeconds / 60L,
        totalSeconds % 60L,
    )
}

internal fun CatalogTrackUi.artistNames(): String = artists
    .joinToString(separator = " · ", transform = CatalogArtistLinkUi::name)

internal fun CatalogAlbumUi.artistNames(): String = artists
    .joinToString(separator = " · ", transform = CatalogArtistLinkUi::name)

private fun CatalogTrackUi.orderLabel(): String? = trackNumber?.let { number ->
    if (discNumber > 1) "$discNumber-$number" else number.toString()
}
