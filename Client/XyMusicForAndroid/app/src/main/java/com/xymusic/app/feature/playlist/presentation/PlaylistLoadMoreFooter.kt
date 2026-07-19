package com.xymusic.app.feature.playlist.presentation

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.xymusic.app.R

@Composable
internal fun PlaylistLoadMoreFooter(
    isLoading: Boolean,
    failed: Boolean,
    onLoadMore: () -> Unit,
    compact: Boolean = false,
) {
    Row(
        modifier =
        Modifier
            .fillMaxWidth()
            .padding(vertical = if (compact) 8.dp else 16.dp),
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        when {
            isLoading -> {
                CircularProgressIndicator(
                    modifier = Modifier.size(20.dp),
                    strokeWidth = 2.dp,
                    color = MaterialTheme.colorScheme.primary,
                )
                Text(
                    text = stringResource(R.string.catalog_loading_more),
                    modifier = Modifier.padding(start = 8.dp),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            failed -> {
                Text(
                    text = stringResource(R.string.catalog_load_more_failed),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                TextButton(onClick = onLoadMore) {
                    Text(stringResource(R.string.catalog_refresh))
                }
            }
            else -> {
                TextButton(onClick = onLoadMore) {
                    Text(stringResource(R.string.playlist_load_more))
                }
            }
        }
    }
}
