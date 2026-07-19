package com.xymusic.app.feature.auth.presentation

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Person
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import com.xymusic.app.R

@Composable
fun SignInScreen(
    uiState: AuthUiState,
    onBack: (() -> Unit)?,
    onSubmit: (username: String, password: String) -> Unit,
    onFieldChanged: (AuthField) -> Unit,
    modifier: Modifier = Modifier,
    onRegister: () -> Unit = {},
) {
    var username by rememberSaveable { mutableStateOf("") }
    var password by rememberSaveable { mutableStateOf("") }

    AuthFormScaffold(
        title = stringResource(R.string.auth_sign_in_title),
        description = stringResource(R.string.app_tagline),
        onBack = onBack,
        modifier = modifier,
    ) {
        AuthTextField(
            value = username,
            onValueChange = {
                username = it
                onFieldChanged(AuthField.Username)
            },
            field = AuthField.Username,
            error = uiState.fieldErrors[AuthField.Username],
            enabled = !uiState.isSubmitting,
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Next),
            leadingIcon = {
                Icon(
                    imageVector = Icons.Default.Person,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.size(20.dp),
                )
            },
            testTag = AuthTestTags.Username,
        )
        PasswordField(
            value = password,
            onValueChange = {
                password = it
                onFieldChanged(AuthField.Password)
            },
            field = AuthField.Password,
            error = uiState.fieldErrors[AuthField.Password],
            enabled = !uiState.isSubmitting,
            imeAction = ImeAction.Done,
            onDone = { if (!uiState.isSubmitting) onSubmit(username, password) },
            leadingIcon = {
                Icon(
                    imageVector = Icons.Default.Lock,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.size(20.dp),
                )
            },
            testTag = AuthTestTags.Password,
        )
        SubmissionButton(
            label = stringResource(R.string.auth_sign_in),
            isSubmitting = uiState.isSubmitting,
            onClick = { onSubmit(username, password) },
        )
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.Center,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = stringResource(R.string.auth_no_account),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                style = MaterialTheme.typography.bodyMedium,
            )
            TextButton(onClick = onRegister, enabled = !uiState.isSubmitting) {
                Text(stringResource(R.string.auth_create_account))
            }
        }
    }
}
