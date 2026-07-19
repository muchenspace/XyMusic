package com.xymusic.app.feature.playlist.presentation

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import com.xymusic.app.R
import com.xymusic.app.core.ui.layout.isCompactLandscape
import com.xymusic.app.feature.playlist.domain.model.PlaylistVisibility
import com.xymusic.app.ui.theme.spacing

@Composable
internal fun PlaylistEditorDialog(
    onDismiss: () -> Unit,
    onSubmit: (String, String?, PlaylistVisibility) -> Unit,
    title: String = stringResource(R.string.playlist_create),
    submitLabel: String = stringResource(R.string.playlist_create),
    initialName: String = "",
    initialDescription: String = "",
    initialVisibility: PlaylistVisibility = PlaylistVisibility.PRIVATE,
) {
    var name by remember(initialName) { mutableStateOf(initialName) }
    var description by remember(initialDescription) { mutableStateOf(initialDescription) }
    var visibility by remember(initialVisibility) { mutableStateOf(initialVisibility) }
    val configuration = LocalConfiguration.current
    val screenWidth = configuration.screenWidthDp.dp
    val screenHeight = configuration.screenHeightDp.dp
    val compactLandscape = isCompactLandscape(screenWidth, screenHeight)
    val submit = {
        onSubmit(
            name.trim(),
            description.trim().takeIf(String::isNotBlank),
            visibility,
        )
    }

    if (compactLandscape) {
        CompactPlaylistEditorDialog(
            title = title,
            submitLabel = submitLabel,
            name = name,
            onNameChange = { if (it.length <= 100) name = it },
            description = description,
            onDescriptionChange = { if (it.length <= 1_000) description = it },
            visibility = visibility,
            onVisibilityChange = { visibility = it },
            onDismiss = onDismiss,
            onSubmit = submit,
        )
    } else {
        AlertDialog(
            onDismissRequest = onDismiss,
            modifier = Modifier.imePadding(),
            title = { Text(title) },
            text = {
                PlaylistEditorForm(
                    name = name,
                    onNameChange = { if (it.length <= 100) name = it },
                    description = description,
                    onDescriptionChange = { if (it.length <= 1_000) description = it },
                    visibility = visibility,
                    onVisibilityChange = { visibility = it },
                )
            },
            confirmButton = {
                TextButton(onClick = submit, enabled = name.isNotBlank()) {
                    Text(submitLabel)
                }
            },
            dismissButton = {
                TextButton(onClick = onDismiss) {
                    Text(stringResource(R.string.common_cancel))
                }
            },
        )
    }
}

@Composable
private fun CompactPlaylistEditorDialog(
    title: String,
    submitLabel: String,
    name: String,
    onNameChange: (String) -> Unit,
    description: String,
    onDescriptionChange: (String) -> Unit,
    visibility: PlaylistVisibility,
    onVisibilityChange: (PlaylistVisibility) -> Unit,
    onDismiss: () -> Unit,
    onSubmit: () -> Unit,
) {
    Dialog(
        onDismissRequest = onDismiss,
        properties =
        DialogProperties(
            usePlatformDefaultWidth = false,
            decorFitsSystemWindows = false,
        ),
    ) {
        Box(
            modifier =
            Modifier
                .fillMaxSize()
                .windowInsetsPadding(WindowInsets.safeDrawing)
                .imePadding()
                .padding(horizontal = 48.dp, vertical = 8.dp),
            contentAlignment = Alignment.Center,
        ) {
            Surface(
                modifier = Modifier.widthIn(max = 560.dp).fillMaxWidth().fillMaxHeight(),
                shape = MaterialTheme.shapes.extraLarge,
                tonalElevation = 6.dp,
            ) {
                Column(
                    modifier =
                    Modifier
                        .fillMaxSize()
                        .verticalScroll(rememberScrollState())
                        .padding(horizontal = 24.dp, vertical = 18.dp),
                    verticalArrangement = Arrangement.spacedBy(MaterialTheme.spacing.medium),
                ) {
                    Text(
                        text = title,
                        style = MaterialTheme.typography.headlineSmall,
                        fontWeight = FontWeight.SemiBold,
                    )
                    PlaylistEditorForm(
                        name = name,
                        onNameChange = onNameChange,
                        description = description,
                        onDescriptionChange = onDescriptionChange,
                        visibility = visibility,
                        onVisibilityChange = onVisibilityChange,
                    )
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.End,
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        TextButton(onClick = onDismiss) {
                            Text(stringResource(R.string.common_cancel))
                        }
                        TextButton(onClick = onSubmit, enabled = name.isNotBlank()) {
                            Text(submitLabel)
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun PlaylistEditorForm(
    name: String,
    onNameChange: (String) -> Unit,
    description: String,
    onDescriptionChange: (String) -> Unit,
    visibility: PlaylistVisibility,
    onVisibilityChange: (PlaylistVisibility) -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(MaterialTheme.spacing.medium)) {
        OutlinedTextField(
            value = name,
            onValueChange = onNameChange,
            modifier = Modifier.fillMaxWidth(),
            label = { Text(stringResource(R.string.playlist_name)) },
            singleLine = true,
        )
        OutlinedTextField(
            value = description,
            onValueChange = onDescriptionChange,
            modifier = Modifier.fillMaxWidth(),
            label = { Text(stringResource(R.string.playlist_description)) },
            minLines = 2,
            maxLines = 4,
        )
        Column(modifier = Modifier.selectableGroup()) {
            PlaylistVisibility.entries.forEach { option ->
                Row(
                    modifier =
                    Modifier
                        .fillMaxWidth()
                        .selectable(
                            selected = visibility == option,
                            onClick = { onVisibilityChange(option) },
                            role = Role.RadioButton,
                        ),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    RadioButton(selected = visibility == option, onClick = null)
                    Text(stringResource(option.labelRes()))
                }
            }
        }
    }
}

internal fun PlaylistVisibility.labelRes(): Int = when (this) {
    PlaylistVisibility.PRIVATE -> R.string.playlist_visibility_private
    PlaylistVisibility.UNLISTED -> R.string.playlist_visibility_unlisted
    PlaylistVisibility.PUBLIC -> R.string.playlist_visibility_public
}
