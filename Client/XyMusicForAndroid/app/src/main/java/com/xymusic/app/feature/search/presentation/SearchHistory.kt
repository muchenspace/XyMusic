package com.xymusic.app.feature.search.presentation

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.outlined.Album
import androidx.compose.material.icons.outlined.MusicNote
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import com.xymusic.app.feature.search.domain.model.SearchScope

@Composable
internal fun SearchIdleContent(
    history: List<SearchHistoryUi>,
    wideLandscape: Boolean = false,
    onSelect: (SearchHistoryUi) -> Unit,
    onDelete: (SearchHistoryUi) -> Unit,
    onClear: () -> Unit,
    onScopeSelected: (SearchScope) -> Unit,
    modifier: Modifier = Modifier,
) {
    val categories = searchBrowseCategories(MaterialTheme.colorScheme.primary)
    val categoryColumns = if (wideLandscape) 4 else 2
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        contentPadding =
        PaddingValues(
            start = if (wideLandscape) 8.dp else 20.dp,
            end = if (wideLandscape) 8.dp else 20.dp,
            bottom = 32.dp,
        ),
    ) {
        searchHistorySection(
            history = history,
            onSelect = onSelect,
            onDelete = onDelete,
            onClear = onClear,
        )
        browseCategorySection(
            categories = categories,
            columns = categoryColumns,
            historyIsEmpty = history.isEmpty(),
            onScopeSelected = onScopeSelected,
        )
    }
}

private fun LazyListScope.searchHistorySection(
    history: List<SearchHistoryUi>,
    onSelect: (SearchHistoryUi) -> Unit,
    onDelete: (SearchHistoryUi) -> Unit,
    onClear: () -> Unit,
) {
    if (history.isEmpty()) return
    item(key = "history-header") {
        SearchHistoryHeader(onClear = onClear)
    }
    items(
        count = history.size,
        key = { index -> "${history[index].scope.name}:${history[index].query}" },
        contentType = { "search-history" },
    ) { index ->
        SearchHistoryRow(
            item = history[index],
            onSelect = onSelect,
            onDelete = onDelete,
        )
    }
}

@Composable
private fun SearchHistoryHeader(onClear: () -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(top = 14.dp, bottom = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = stringResource(R.string.search_recent),
            modifier = Modifier.weight(1f),
            style = MaterialTheme.typography.headlineSmall,
            fontWeight = FontWeight.Bold,
        )
        TextButton(
            onClick = onClear,
            modifier = Modifier.testTag(SearchTestTags.ClearHistory),
        ) {
            Text(
                stringResource(R.string.search_clear_history),
                color = MaterialTheme.colorScheme.primary,
            )
        }
    }
}

@Composable
private fun SearchHistoryRow(
    item: SearchHistoryUi,
    onSelect: (SearchHistoryUi) -> Unit,
    onDelete: (SearchHistoryUi) -> Unit,
) {
    Row(
        modifier =
        Modifier
            .fillMaxWidth()
            .clickable(role = Role.Button) { onSelect(item) }
            .testTag(SearchTestTags.historyItem(item))
            .padding(start = 2.dp, top = 9.dp, bottom = 9.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            imageVector = Icons.Outlined.Search,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.size(20.dp),
        )
        Spacer(modifier = Modifier.width(12.dp))
        SearchHistoryText(item = item, modifier = Modifier.weight(1f))
        IconButton(onClick = { onDelete(item) }, modifier = Modifier.size(42.dp)) {
            Icon(
                imageVector = Icons.Default.Close,
                contentDescription = stringResource(R.string.search_delete_history),
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(18.dp),
            )
        }
    }
    HorizontalDivider(
        modifier = Modifier.padding(start = 34.dp),
        thickness = 0.5.dp,
        color = MaterialTheme.colorScheme.outlineVariant,
    )
}

@Composable
private fun SearchHistoryText(item: SearchHistoryUi, modifier: Modifier = Modifier) {
    Column(modifier = modifier) {
        Text(
            text = item.query,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            style = MaterialTheme.typography.bodyLarge,
        )
        if (item.searchedAt.isNotBlank()) {
            Text(
                text = item.searchedAt,
                maxLines = 1,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                style = MaterialTheme.typography.bodySmall,
            )
        }
    }
}

private fun LazyListScope.browseCategorySection(
    categories: List<BrowseCategory>,
    columns: Int,
    historyIsEmpty: Boolean,
    onScopeSelected: (SearchScope) -> Unit,
) {
    item(key = "browse-heading") {
        Text(
            text = stringResource(R.string.search_browse_categories),
            modifier = Modifier.padding(top = if (historyIsEmpty) 18.dp else 30.dp, bottom = 12.dp),
            style = MaterialTheme.typography.headlineSmall,
            fontWeight = FontWeight.Bold,
        )
    }
    items(
        count = (categories.size + columns - 1) / columns,
        key = { row -> "browse-row-$row" },
    ) { row ->
        BrowseCategoryRow(
            categories = categories,
            firstIndex = row * columns,
            columns = columns,
            onScopeSelected = onScopeSelected,
        )
    }
}

@Composable
private fun BrowseCategoryRow(
    categories: List<BrowseCategory>,
    firstIndex: Int,
    columns: Int,
    onScopeSelected: (SearchScope) -> Unit,
) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(bottom = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        repeat(columns) { column ->
            val category = categories.getOrNull(firstIndex + column)
            if (category != null) {
                BrowseCategoryCard(
                    category = category,
                    onClick = { onScopeSelected(category.scope) },
                    modifier = Modifier.weight(1f),
                )
            } else {
                Spacer(modifier = Modifier.weight(1f))
            }
        }
    }
}

@Composable
private fun BrowseCategoryCard(category: BrowseCategory, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Row(
        modifier =
        modifier
            .height(92.dp)
            .clip(RoundedCornerShape(10.dp))
            .background(
                Brush.linearGradient(
                    colors = listOf(category.color, category.color.copy(alpha = 0.72f)),
                ),
            ).clickable(role = Role.Button, onClick = onClick)
            .padding(14.dp),
        verticalAlignment = Alignment.Bottom,
    ) {
        Text(
            text = stringResource(category.labelRes),
            modifier = Modifier.weight(1f),
            color = Color.White,
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.Bold,
        )
        Icon(
            imageVector = category.icon,
            contentDescription = null,
            tint = Color.White.copy(alpha = 0.9f),
            modifier = Modifier.size(28.dp),
        )
    }
}

private fun searchBrowseCategories(primaryColor: Color): List<BrowseCategory> = listOf(
    BrowseCategory(SearchScope.ALL, R.string.search_scope_all, Icons.Outlined.Search, primaryColor),
    BrowseCategory(SearchScope.TRACKS, R.string.search_scope_tracks, Icons.Outlined.MusicNote, Color(0xFF8E5CE6)),
    BrowseCategory(SearchScope.ALBUMS, R.string.search_scope_albums, Icons.Outlined.Album, Color(0xFFED7D31)),
    BrowseCategory(SearchScope.ARTISTS, R.string.search_scope_artists, Icons.Outlined.Person, Color(0xFF3478F6)),
)

private data class BrowseCategory(val scope: SearchScope, val labelRes: Int, val icon: ImageVector, val color: Color)

@Composable
internal fun SearchHistory(
    history: List<SearchHistoryUi>,
    onSelect: (SearchHistoryUi) -> Unit,
    onDelete: (SearchHistoryUi) -> Unit,
    onClear: () -> Unit,
    modifier: Modifier = Modifier,
) {
    SearchIdleContent(
        history = history,
        onSelect = onSelect,
        onDelete = onDelete,
        onClear = onClear,
        onScopeSelected = {},
        modifier = modifier,
    )
}
