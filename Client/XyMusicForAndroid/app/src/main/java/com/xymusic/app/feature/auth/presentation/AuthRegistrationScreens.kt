package com.xymusic.app.feature.auth.presentation

import androidx.compose.foundation.layout.size
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Person
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import com.xymusic.app.R

@Composable
fun RegisterScreen(
    uiState: AuthUiState,
    onBack: () -> Unit,
    onSubmit: (
        username: String,
        password: String,
        confirmPassword: String,
    ) -> Unit,
    onFieldChanged: (AuthField) -> Unit,
    modifier: Modifier = Modifier,
) {
    var username by rememberSaveable { mutableStateOf("") }
    var password by rememberSaveable { mutableStateOf("") }
    var confirmPassword by rememberSaveable { mutableStateOf("") }

    AuthFormScaffold(
        title = stringResource(R.string.auth_register_title),
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
            imeAction = ImeAction.Next,
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
        PasswordField(
            value = confirmPassword,
            onValueChange = {
                confirmPassword = it
                onFieldChanged(AuthField.ConfirmPassword)
            },
            field = AuthField.ConfirmPassword,
            error = uiState.fieldErrors[AuthField.ConfirmPassword],
            enabled = !uiState.isSubmitting,
            imeAction = ImeAction.Done,
            onDone = {
                if (!uiState.isSubmitting) {
                    onSubmit(username, password, confirmPassword)
                }
            },
            leadingIcon = {
                Icon(
                    imageVector = Icons.Default.Lock,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.size(20.dp),
                )
            },
            testTag = AuthTestTags.ConfirmPassword,
        )
        SubmissionButton(
            label = stringResource(R.string.auth_create_account),
            isSubmitting = uiState.isSubmitting,
            onClick = { onSubmit(username, password, confirmPassword) },
        )
    }
}
