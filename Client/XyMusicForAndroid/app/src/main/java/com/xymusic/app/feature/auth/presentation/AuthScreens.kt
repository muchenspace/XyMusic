package com.xymusic.app.feature.auth.presentation

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Login
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import com.xymusic.app.core.ui.layout.isCompactLandscape
import com.xymusic.app.core.ui.layout.isWideLandscape
import com.xymusic.app.ui.theme.spacing

internal object AuthTestTags {
    const val Username = "auth_username"
    const val DisplayName = "auth_display_name"
    const val Password = "auth_password"
    const val ConfirmPassword = "auth_confirm_password"
    const val Submit = "auth_submit"
    const val EntryBrand = "auth_entry_brand"
    const val EntryActions = "auth_entry_actions"
    const val FormBrand = "auth_form_brand"
    const val FormFields = "auth_form_fields"
}

@Composable
fun AuthEntryScreen(
    onSignIn: () -> Unit,
    onRegister: () -> Unit,
    modifier: Modifier = Modifier,
    serverAddress: String? = null,
    onEditServer: () -> Unit = {},
) {
    val colorScheme = MaterialTheme.colorScheme
    BoxWithConstraints(
        modifier =
        modifier
            .fillMaxSize()
            .background(colorScheme.background),
    ) {
        val wideLandscape = isWideLandscape(maxWidth, maxHeight)
        val compactLandscape = isCompactLandscape(maxWidth, maxHeight)
        if (wideLandscape) {
            Row(
                modifier =
                Modifier
                    .align(Alignment.Center)
                    .widthIn(max = 960.dp)
                    .fillMaxWidth()
                    .fillMaxHeight()
                    .padding(
                        horizontal = MaterialTheme.spacing.large,
                        vertical =
                        if (compactLandscape) {
                            MaterialTheme.spacing.small
                        } else {
                            MaterialTheme.spacing.large
                        },
                    ),
                horizontalArrangement = Arrangement.spacedBy(MaterialTheme.spacing.extraLarge),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                AuthEntryBrand(
                    compact = compactLandscape,
                    modifier =
                    Modifier
                        .weight(1f)
                        .fillMaxHeight()
                        .testTag(AuthTestTags.EntryBrand),
                )
                AuthEntryActions(
                    onSignIn = onSignIn,
                    onRegister = onRegister,
                    serverAddress = serverAddress,
                    onEditServer = onEditServer,
                    modifier =
                    Modifier
                        .weight(1f)
                        .widthIn(max = 420.dp)
                        .testTag(AuthTestTags.EntryActions),
                )
            }
        } else {
            Box(
                modifier =
                Modifier
                    .fillMaxSize()
                    .padding(MaterialTheme.spacing.large),
                contentAlignment = Alignment.Center,
            ) {
                Column(
                    modifier =
                    Modifier
                        .widthIn(max = 420.dp)
                        .fillMaxWidth(),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Spacer(modifier = Modifier.height(60.dp))
                    AuthEntryBrand(compact = false)
                    Spacer(modifier = Modifier.weight(1f))
                    AuthEntryActions(
                        onSignIn = onSignIn,
                        onRegister = onRegister,
                        serverAddress = serverAddress,
                        onEditServer = onEditServer,
                    )
                    Spacer(modifier = Modifier.height(32.dp))
                }
            }
        }
    }
}

@Composable
private fun AuthEntryBrand(compact: Boolean, modifier: Modifier = Modifier) {
    val colorScheme = MaterialTheme.colorScheme
    Column(
        modifier = modifier,
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Box(
            modifier =
            Modifier
                .size(if (compact) 72.dp else 100.dp)
                .clip(MaterialTheme.shapes.extraLarge),
            contentAlignment = Alignment.Center,
        ) {
            Image(
                painter = painterResource(R.drawable.xymusic),
                contentDescription = null,
                modifier = Modifier.fillMaxSize(),
                contentScale = ContentScale.Crop,
            )
        }
        Spacer(modifier = Modifier.height(if (compact) 12.dp else 24.dp))
        Text(
            text = stringResource(R.string.app_name),
            style =
            if (compact) {
                MaterialTheme.typography.headlineLarge
            } else {
                MaterialTheme.typography.displayMedium
            },
            fontWeight = FontWeight.Bold,
            color = colorScheme.onBackground,
        )
        Spacer(modifier = Modifier.height(if (compact) 4.dp else 8.dp))
        Text(
            text = stringResource(R.string.app_tagline),
            color = colorScheme.onSurfaceVariant,
            style = if (compact) MaterialTheme.typography.bodyMedium else MaterialTheme.typography.bodyLarge,
            textAlign = TextAlign.Center,
        )
    }
}

@Composable
private fun AuthEntryActions(
    onSignIn: () -> Unit,
    onRegister: () -> Unit,
    serverAddress: String?,
    onEditServer: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val colorScheme = MaterialTheme.colorScheme
    Column(
        modifier = modifier.fillMaxWidth(),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Button(
            onClick = onSignIn,
            modifier =
            Modifier
                .fillMaxWidth()
                .height(52.dp),
            colors =
            ButtonDefaults.buttonColors(
                containerColor = colorScheme.surfaceContainerHighest,
                contentColor = colorScheme.onSurface,
            ),
            shape = MaterialTheme.shapes.extraLarge,
        ) {
            Icon(
                imageVector = Icons.AutoMirrored.Filled.Login,
                contentDescription = null,
                modifier = Modifier.size(20.dp),
            )
            Spacer(modifier = Modifier.width(8.dp))
            Text(
                text = stringResource(R.string.auth_sign_in),
                style = MaterialTheme.typography.labelLarge,
            )
        }
        Spacer(modifier = Modifier.height(12.dp))
        OutlinedButton(
            onClick = onRegister,
            modifier =
            Modifier
                .fillMaxWidth()
                .height(52.dp),
            colors =
            ButtonDefaults.outlinedButtonColors(
                contentColor = colorScheme.primary,
            ),
            shape = MaterialTheme.shapes.extraLarge,
        ) {
            Text(
                text = stringResource(R.string.auth_create_account),
                style = MaterialTheme.typography.labelLarge,
            )
        }
        if (serverAddress != null) {
            Spacer(modifier = Modifier.height(8.dp))
            TextButton(onClick = onEditServer) {
                Text(
                    text = stringResource(R.string.auth_current_server, serverAddress),
                    style = MaterialTheme.typography.bodySmall,
                    textAlign = TextAlign.Center,
                )
            }
        }
    }
}
