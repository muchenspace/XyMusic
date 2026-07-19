package com.xymusic.app.feature.settings.presentation

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import com.xymusic.app.R
import com.xymusic.app.core.ui.layout.isCompactLandscape
import com.xymusic.app.core.ui.layout.isWideLandscape
import com.xymusic.app.feature.settings.domain.model.UserProfile
import com.xymusic.app.ui.theme.spacing

internal object SettingsDialogTestTags {
    const val DisplayName = "settings_dialog_display_name"
    const val Bio = "settings_dialog_bio"
    const val Confirm = "settings_dialog_confirm"
}

@Composable
internal fun EditProfileDialog(profile: UserProfile, onDismiss: () -> Unit, onSave: (String, String?) -> Unit) {
    var displayName by remember(profile.id) { mutableStateOf(profile.displayName) }
    var bio by remember(profile.id) { mutableStateOf(profile.bio.orEmpty()) }
    BoxWithConstraints(modifier = Modifier.fillMaxSize()) {
        val wideLandscape = isWideLandscape(maxWidth, maxHeight)
        val compactLandscape = isCompactLandscape(maxWidth, maxHeight)
        if (compactLandscape) {
            CompactEditProfileDialog(
                displayName = displayName,
                onDisplayNameChange = { displayName = it },
                bio = bio,
                onBioChange = { bio = it },
                onDismiss = onDismiss,
                onSave = { onSave(displayName, bio) },
            )
        } else {
            AlertDialog(
                onDismissRequest = onDismiss,
                title = { Text(stringResource(R.string.settings_edit_profile)) },
                text = {
                    if (wideLandscape) {
                        Row(
                            modifier =
                            Modifier
                                .fillMaxWidth()
                                .verticalScroll(rememberScrollState())
                                .imePadding(),
                            horizontalArrangement = Arrangement.spacedBy(MaterialTheme.spacing.medium),
                        ) {
                            DisplayNameField(
                                value = displayName,
                                onValueChange = { displayName = it },
                                modifier = Modifier.weight(1f),
                            )
                            BioField(
                                value = bio,
                                onValueChange = { bio = it },
                                minLines = 3,
                                maxLines = 5,
                                modifier = Modifier.weight(1f),
                            )
                        }
                    } else {
                        Column(
                            modifier =
                            Modifier
                                .verticalScroll(rememberScrollState())
                                .imePadding(),
                            verticalArrangement = Arrangement.spacedBy(MaterialTheme.spacing.medium),
                        ) {
                            DisplayNameField(
                                value = displayName,
                                onValueChange = { displayName = it },
                            )
                            BioField(
                                value = bio,
                                onValueChange = { bio = it },
                                minLines = 3,
                                maxLines = 6,
                            )
                        }
                    }
                },
                confirmButton = {
                    TextButton(
                        onClick = { onSave(displayName, bio) },
                        enabled = displayName.isNotBlank(),
                        modifier = Modifier.testTag(SettingsDialogTestTags.Confirm),
                    ) { Text(stringResource(R.string.common_confirm)) }
                },
                dismissButton = {
                    TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
                },
                modifier = Modifier.widthIn(max = if (wideLandscape) 720.dp else 560.dp),
            )
        }
    }
}

@Composable
private fun CompactEditProfileDialog(
    displayName: String,
    onDisplayNameChange: (String) -> Unit,
    bio: String,
    onBioChange: (String) -> Unit,
    onDismiss: () -> Unit,
    onSave: () -> Unit,
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
                .safeDrawingPadding()
                .imePadding()
                .padding(horizontal = 48.dp, vertical = 8.dp),
            contentAlignment = Alignment.Center,
        ) {
            Surface(
                modifier = Modifier.widthIn(max = 720.dp).fillMaxWidth().fillMaxHeight(),
                shape = MaterialTheme.shapes.extraLarge,
                tonalElevation = 6.dp,
            ) {
                Column(
                    modifier =
                    Modifier
                        .fillMaxSize()
                        .verticalScroll(rememberScrollState())
                        .padding(horizontal = 24.dp, vertical = 14.dp),
                    verticalArrangement = Arrangement.spacedBy(MaterialTheme.spacing.small),
                ) {
                    Text(
                        text = stringResource(R.string.settings_edit_profile),
                        style = MaterialTheme.typography.headlineSmall,
                    )
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.spacedBy(MaterialTheme.spacing.medium),
                    ) {
                        DisplayNameField(
                            value = displayName,
                            onValueChange = onDisplayNameChange,
                            modifier = Modifier.weight(1f),
                        )
                        BioField(
                            value = bio,
                            onValueChange = onBioChange,
                            minLines = 2,
                            maxLines = 3,
                            modifier = Modifier.weight(1f),
                        )
                    }
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.End,
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        TextButton(onClick = onDismiss) {
                            Text(stringResource(R.string.common_cancel))
                        }
                        TextButton(
                            onClick = onSave,
                            enabled = displayName.isNotBlank(),
                            modifier = Modifier.testTag(SettingsDialogTestTags.Confirm),
                        ) {
                            Text(stringResource(R.string.common_confirm))
                        }
                    }
                }
            }
        }
    }
}

@Composable
internal fun ConfirmationDialog(
    title: String,
    message: String,
    confirmLabel: String,
    onDismiss: () -> Unit,
    onConfirm: () -> Unit,
) {
    BoxWithConstraints(modifier = Modifier.fillMaxSize()) {
        val wideLandscape = isWideLandscape(maxWidth, maxHeight)
        AlertDialog(
            onDismissRequest = onDismiss,
            title = { Text(title) },
            text = {
                Column(modifier = Modifier.verticalScroll(rememberScrollState())) {
                    Text(message)
                }
            },
            confirmButton = {
                TextButton(
                    onClick = onConfirm,
                    modifier = Modifier.testTag(SettingsDialogTestTags.Confirm),
                ) {
                    Text(confirmLabel)
                }
            },
            dismissButton = {
                TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
            },
            modifier = Modifier.widthIn(max = if (wideLandscape) 640.dp else 560.dp),
        )
    }
}

@Composable
private fun DisplayNameField(value: String, onValueChange: (String) -> Unit, modifier: Modifier = Modifier) {
    OutlinedTextField(
        value = value,
        onValueChange = { if (it.length <= 64) onValueChange(it) },
        modifier =
        modifier
            .fillMaxWidth()
            .testTag(SettingsDialogTestTags.DisplayName),
        label = { Text(stringResource(R.string.settings_display_name)) },
        singleLine = true,
    )
}

@Composable
private fun BioField(
    value: String,
    onValueChange: (String) -> Unit,
    minLines: Int,
    maxLines: Int,
    modifier: Modifier = Modifier,
) {
    OutlinedTextField(
        value = value,
        onValueChange = { if (it.length <= 500) onValueChange(it) },
        modifier =
        modifier
            .fillMaxWidth()
            .testTag(SettingsDialogTestTags.Bio),
        label = { Text(stringResource(R.string.settings_bio)) },
        minLines = minLines,
        maxLines = maxLines,
    )
}
