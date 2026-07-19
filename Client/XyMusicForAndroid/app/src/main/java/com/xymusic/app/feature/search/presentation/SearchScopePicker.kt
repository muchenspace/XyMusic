package com.xymusic.app.feature.search.presentation

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import com.xymusic.app.feature.search.domain.model.SearchScope

private fun SearchScope.labelRes(): Int = when (this) {
    SearchScope.ALL -> R.string.search_scope_all
    SearchScope.TRACKS -> R.string.search_scope_tracks
    SearchScope.ARTISTS -> R.string.search_scope_artists
    SearchScope.ALBUMS -> R.string.search_scope_albums
}

@Composable
internal fun SearchScopePicker(selectedScope: SearchScope, onScopeSelected: (SearchScope) -> Unit) {
    LazyRow(
        contentPadding = PaddingValues(horizontal = 20.dp, vertical = 6.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        items(SearchScope.entries.size, key = { SearchScope.entries[it].name }) { index ->
            val scope = SearchScope.entries[index]
            val selected = scope == selectedScope
            Surface(
                modifier = Modifier.clickable(role = Role.Tab) { onScopeSelected(scope) },
                shape = CircleShape,
                color =
                if (selected) {
                    MaterialTheme.colorScheme.primary
                } else {
                    MaterialTheme.colorScheme.surfaceContainerHigh
                },
                contentColor =
                if (selected) {
                    androidx.compose.ui.graphics.Color.White
                } else {
                    MaterialTheme.colorScheme.onSurface
                },
            ) {
                Text(
                    text = stringResource(scope.labelRes()),
                    modifier = Modifier.padding(horizontal = 15.dp, vertical = 7.dp),
                    style = MaterialTheme.typography.labelLarge,
                    fontWeight = if (selected) FontWeight.Bold else FontWeight.Medium,
                )
            }
        }
    }
}
