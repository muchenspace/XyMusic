package com.xymusic.app.feature.server.presentation

import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import com.xymusic.app.R
import com.xymusic.app.core.network.ServerEndpoint
import com.xymusic.app.core.network.ServerProtocol
import com.xymusic.app.core.ui.layout.isCompactLandscape
import com.xymusic.app.core.ui.layout.isWideLandscape
import com.xymusic.app.ui.theme.spacing

object ServerConfigTestTags {
    const val Host = "server_config_host"
    const val Port = "server_config_port"
    const val Https = "server_config_https"
    const val Save = "server_config_save"
    const val ConfirmHttp = "server_config_confirm_http"
    const val LandscapeIntro = "server_config_landscape_intro"
    const val LandscapeForm = "server_config_landscape_form"
}

@Composable
fun ServerSetupScreen(onSave: (ServerEndpoint) -> Unit, modifier: Modifier = Modifier) {
    var host by rememberSaveable { mutableStateOf("") }
    var port by rememberSaveable { mutableStateOf("") }
    var useHttps by rememberSaveable { mutableStateOf(true) }
    var showError by rememberSaveable { mutableStateOf(false) }
    var pendingHttpEndpoint by remember { mutableStateOf<ServerEndpoint?>(null) }
    val onHostChanged: (String) -> Unit = {
        host = it
        showError = false
    }
    val onPortChanged: (String) -> Unit = {
        port = it.filter(Char::isDigit).take(5)
        showError = false
    }
    val onHttpsChanged: (Boolean) -> Unit = {
        useHttps = it
        showError = false
    }
    val saveEndpoint = {
        handleEndpointSave(
            host = host,
            port = port,
            useHttps = useHttps,
            onInvalid = { showError = true },
            onHttp = { pendingHttpEndpoint = it },
            onHttps = onSave,
        )
    }

    Surface(modifier = modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        ServerSetupContent(
            host = host,
            port = port,
            useHttps = useHttps,
            showError = showError,
            onHostChanged = onHostChanged,
            onPortChanged = onPortChanged,
            onHttpsChanged = onHttpsChanged,
            onSave = saveEndpoint,
        )
    }
    pendingHttpEndpoint?.let { endpoint ->
        HttpWarningDialog(
            onDismiss = { pendingHttpEndpoint = null },
            onConfirm = {
                pendingHttpEndpoint = null
                onSave(endpoint)
            },
        )
    }
}

@Composable
private fun ServerSetupContent(
    host: String,
    port: String,
    useHttps: Boolean,
    showError: Boolean,
    onHostChanged: (String) -> Unit,
    onPortChanged: (String) -> Unit,
    onHttpsChanged: (Boolean) -> Unit,
    onSave: () -> Unit,
) {
    BoxWithConstraints(
        modifier =
        Modifier
            .fillMaxSize()
            .safeDrawingPadding()
            .imePadding(),
    ) {
        val wideLandscape = isWideLandscape(maxWidth, maxHeight)
        val compactLandscape = isCompactLandscape(maxWidth, maxHeight)
        if (wideLandscape) {
            LandscapeServerSetupContent(
                host = host,
                port = port,
                useHttps = useHttps,
                showError = showError,
                compact = compactLandscape,
                onHostChanged = onHostChanged,
                onPortChanged = onPortChanged,
                onHttpsChanged = onHttpsChanged,
                onSave = onSave,
                modifier = Modifier.align(Alignment.Center),
            )
        } else {
            PortraitServerSetupContent(
                host = host,
                port = port,
                useHttps = useHttps,
                showError = showError,
                onHostChanged = onHostChanged,
                onPortChanged = onPortChanged,
                onHttpsChanged = onHttpsChanged,
                onSave = onSave,
            )
        }
    }
}

@Composable
private fun LandscapeServerSetupContent(
    host: String,
    port: String,
    useHttps: Boolean,
    showError: Boolean,
    compact: Boolean,
    onHostChanged: (String) -> Unit,
    onPortChanged: (String) -> Unit,
    onHttpsChanged: (Boolean) -> Unit,
    onSave: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier =
        modifier
            .widthIn(max = 960.dp)
            .fillMaxWidth()
            .fillMaxHeight()
            .padding(
                horizontal = MaterialTheme.spacing.large,
                vertical =
                if (compact) {
                    MaterialTheme.spacing.small
                } else {
                    MaterialTheme.spacing.extraLarge
                },
            ),
        horizontalArrangement = Arrangement.spacedBy(MaterialTheme.spacing.extraLarge),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        ServerSetupIntro(
            modifier =
            Modifier
                .weight(1f)
                .testTag(ServerConfigTestTags.LandscapeIntro),
        )
        ServerSetupForm(
            host = host,
            port = port,
            useHttps = useHttps,
            showError = showError,
            compact = compact,
            onHostChanged = onHostChanged,
            onPortChanged = onPortChanged,
            onHttpsChanged = onHttpsChanged,
            onSave = onSave,
            modifier =
            Modifier
                .weight(1f)
                .widthIn(max = 440.dp)
                .fillMaxHeight()
                .verticalScroll(rememberScrollState())
                .testTag(ServerConfigTestTags.LandscapeForm),
        )
    }
}

@Composable
private fun PortraitServerSetupContent(
    host: String,
    port: String,
    useHttps: Boolean,
    showError: Boolean,
    onHostChanged: (String) -> Unit,
    onPortChanged: (String) -> Unit,
    onHttpsChanged: (Boolean) -> Unit,
    onSave: () -> Unit,
) {
    Column(
        modifier =
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(
                horizontal = MaterialTheme.spacing.large,
                vertical = MaterialTheme.spacing.extraLarge,
            ),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Top,
    ) {
        Column(
            modifier =
            Modifier
                .widthIn(max = 420.dp)
                .fillMaxWidth(),
        ) {
            ServerSetupIntro()
            Spacer(modifier = Modifier.height(MaterialTheme.spacing.extraLarge))
            ServerSetupForm(
                host = host,
                port = port,
                useHttps = useHttps,
                showError = showError,
                compact = false,
                onHostChanged = onHostChanged,
                onPortChanged = onPortChanged,
                onHttpsChanged = onHttpsChanged,
                onSave = onSave,
            )
        }
    }
}

@Composable
private fun ServerSetupIntro(modifier: Modifier = Modifier) {
    Column(modifier = modifier, verticalArrangement = Arrangement.Center) {
        Text(
            text = stringResource(R.string.server_setup_title),
            style = MaterialTheme.typography.titleLarge,
        )
        Spacer(modifier = Modifier.height(MaterialTheme.spacing.small))
        Text(
            text = stringResource(R.string.server_setup_description),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.bodyMedium,
        )
    }
}

@Composable
private fun ServerSetupForm(
    host: String,
    port: String,
    useHttps: Boolean,
    showError: Boolean,
    compact: Boolean,
    onHostChanged: (String) -> Unit,
    onPortChanged: (String) -> Unit,
    onHttpsChanged: (Boolean) -> Unit,
    onSave: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier,
        verticalArrangement = Arrangement.Center,
    ) {
        ServerEndpointFields(
            host = host,
            port = port,
            useHttps = useHttps,
            showError = showError,
            onHostChanged = onHostChanged,
            onPortChanged = onPortChanged,
            onHttpsChanged = onHttpsChanged,
        )
        Spacer(
            modifier =
            Modifier.height(
                if (compact) MaterialTheme.spacing.small else MaterialTheme.spacing.large,
            ),
        )
        Button(
            onClick = onSave,
            modifier =
            Modifier
                .fillMaxWidth()
                .height(52.dp)
                .testTag(ServerConfigTestTags.Save),
        ) {
            Text(stringResource(R.string.server_save_and_continue))
        }
    }
}

@Composable
fun ServerEndpointDialog(currentEndpoint: ServerEndpoint, onDismiss: () -> Unit, onSave: (ServerEndpoint) -> Unit) {
    var host by rememberSaveable(currentEndpoint) { mutableStateOf(currentEndpoint.host) }
    var port by rememberSaveable(currentEndpoint) { mutableStateOf(currentEndpoint.port.toString()) }
    var useHttps by rememberSaveable(currentEndpoint) {
        mutableStateOf(
            currentEndpoint.protocol == ServerProtocol.HTTPS,
        )
    }
    var showError by rememberSaveable(currentEndpoint) { mutableStateOf(false) }
    var pendingHttpEndpoint by remember(currentEndpoint) {
        mutableStateOf<ServerEndpoint?>(null)
    }

    if (pendingHttpEndpoint != null) {
        HttpWarningDialog(
            onDismiss = { pendingHttpEndpoint = null },
            onConfirm = {
                val endpoint = checkNotNull(pendingHttpEndpoint)
                pendingHttpEndpoint = null
                onSave(endpoint)
            },
        )
        return
    }

    BoxWithConstraints(modifier = Modifier.fillMaxSize()) {
        val wideLandscape = isWideLandscape(maxWidth, maxHeight)
        val compactLandscape = isCompactLandscape(maxWidth, maxHeight)
        val onHostChanged: (String) -> Unit = {
            host = it
            showError = false
        }
        val onPortChanged: (String) -> Unit = {
            port = it.filter(Char::isDigit).take(5)
            showError = false
        }
        val onHttpsChanged: (Boolean) -> Unit = {
            useHttps = it
            showError = false
        }
        val onConfirm = {
            handleEndpointSave(
                host = host,
                port = port,
                useHttps = useHttps,
                onInvalid = { showError = true },
                onHttp = { pendingHttpEndpoint = it },
                onHttps = onSave,
            )
        }
        if (compactLandscape) {
            CompactServerEndpointDialog(
                host = host,
                port = port,
                useHttps = useHttps,
                showError = showError,
                onHostChanged = onHostChanged,
                onPortChanged = onPortChanged,
                onHttpsChanged = onHttpsChanged,
                onDismiss = onDismiss,
                onConfirm = onConfirm,
            )
        } else {
            AlertDialog(
                onDismissRequest = onDismiss,
                title = { Text(stringResource(R.string.server_edit_title)) },
                text = {
                    Column(
                        modifier =
                        Modifier
                            .verticalScroll(rememberScrollState())
                            .imePadding(),
                    ) {
                        Text(
                            text = stringResource(R.string.server_change_warning),
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            style = MaterialTheme.typography.bodySmall,
                        )
                        Spacer(modifier = Modifier.height(MaterialTheme.spacing.medium))
                        ServerEndpointFields(
                            host = host,
                            port = port,
                            useHttps = useHttps,
                            showError = showError,
                            horizontal = wideLandscape,
                            onHostChanged = onHostChanged,
                            onPortChanged = onPortChanged,
                            onHttpsChanged = onHttpsChanged,
                        )
                    }
                },
                confirmButton = {
                    TextButton(
                        onClick = onConfirm,
                        modifier = Modifier.testTag(ServerConfigTestTags.Save),
                    ) {
                        Text(stringResource(R.string.common_confirm))
                    }
                },
                dismissButton = {
                    TextButton(onClick = onDismiss) {
                        Text(stringResource(R.string.common_cancel))
                    }
                },
                modifier = Modifier.widthIn(max = if (wideLandscape) 680.dp else 560.dp),
            )
        }
    }
}

@Composable
private fun CompactServerEndpointDialog(
    host: String,
    port: String,
    useHttps: Boolean,
    showError: Boolean,
    onHostChanged: (String) -> Unit,
    onPortChanged: (String) -> Unit,
    onHttpsChanged: (Boolean) -> Unit,
    onDismiss: () -> Unit,
    onConfirm: () -> Unit,
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
                modifier = Modifier.widthIn(max = 680.dp).fillMaxWidth().fillMaxHeight(),
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
                        text = stringResource(R.string.server_edit_title),
                        style = MaterialTheme.typography.headlineSmall,
                    )
                    Text(
                        text = stringResource(R.string.server_change_warning),
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        style = MaterialTheme.typography.bodySmall,
                    )
                    ServerEndpointFields(
                        host = host,
                        port = port,
                        useHttps = useHttps,
                        showError = showError,
                        horizontal = true,
                        onHostChanged = onHostChanged,
                        onPortChanged = onPortChanged,
                        onHttpsChanged = onHttpsChanged,
                    )
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.End,
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        TextButton(onClick = onDismiss) {
                            Text(stringResource(R.string.common_cancel))
                        }
                        TextButton(
                            onClick = onConfirm,
                            modifier = Modifier.testTag(ServerConfigTestTags.Save),
                        ) {
                            Text(stringResource(R.string.common_confirm))
                        }
                    }
                }
            }
        }
    }
}

private fun handleEndpointSave(
    host: String,
    port: String,
    useHttps: Boolean,
    onInvalid: () -> Unit,
    onHttp: (ServerEndpoint) -> Unit,
    onHttps: (ServerEndpoint) -> Unit,
) {
    val endpoint =
        parseEndpoint(
            host = host,
            port = port,
            useHttps = useHttps,
        )
    when (endpoint?.protocol) {
        ServerProtocol.HTTP -> onHttp(endpoint)
        ServerProtocol.HTTPS -> onHttps(endpoint)
        null -> onInvalid()
    }
}

@Composable
private fun HttpWarningDialog(onDismiss: () -> Unit, onConfirm: () -> Unit) {
    BoxWithConstraints(modifier = Modifier.fillMaxSize()) {
        val wideLandscape = isWideLandscape(maxWidth, maxHeight)
        AlertDialog(
            onDismissRequest = onDismiss,
            title = { Text(stringResource(R.string.server_http_warning_title)) },
            text = {
                Column(modifier = Modifier.verticalScroll(rememberScrollState())) {
                    Text(stringResource(R.string.server_http_warning_message))
                }
            },
            confirmButton = {
                TextButton(
                    onClick = onConfirm,
                    modifier = Modifier.testTag(ServerConfigTestTags.ConfirmHttp),
                ) {
                    Text(stringResource(R.string.server_http_warning_confirm))
                }
            },
            dismissButton = {
                TextButton(onClick = onDismiss) {
                    Text(stringResource(R.string.common_cancel))
                }
            },
            modifier = Modifier.widthIn(max = if (wideLandscape) 640.dp else 560.dp),
        )
    }
}

@Composable
private fun ServerEndpointFields(
    host: String,
    port: String,
    useHttps: Boolean,
    showError: Boolean,
    onHostChanged: (String) -> Unit,
    onPortChanged: (String) -> Unit,
    onHttpsChanged: (Boolean) -> Unit,
    modifier: Modifier = Modifier,
    horizontal: Boolean = false,
) {
    Column(modifier = modifier.fillMaxWidth()) {
        if (horizontal) {
            Row(horizontalArrangement = Arrangement.spacedBy(MaterialTheme.spacing.small)) {
                ServerHostField(
                    host = host,
                    showError = showError,
                    onHostChanged = onHostChanged,
                    modifier = Modifier.weight(1.7f),
                )
                ServerPortField(
                    port = port,
                    showError = showError,
                    onPortChanged = onPortChanged,
                    modifier = Modifier.weight(1f),
                )
            }
        } else {
            ServerHostField(
                host = host,
                showError = showError,
                onHostChanged = onHostChanged,
            )
            Spacer(modifier = Modifier.height(MaterialTheme.spacing.small))
            ServerPortField(
                port = port,
                showError = showError,
                onPortChanged = onPortChanged,
            )
        }
        Row(
            modifier =
            Modifier
                .fillMaxWidth()
                .clickable(
                    enabled = true,
                    onClick = { onHttpsChanged(!useHttps) },
                ).padding(vertical = MaterialTheme.spacing.small),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Text(
                text = stringResource(R.string.server_use_https),
                modifier = Modifier.weight(1f),
                style = MaterialTheme.typography.titleMedium,
            )
            Switch(
                checked = useHttps,
                onCheckedChange = onHttpsChanged,
                enabled = true,
                modifier = Modifier.testTag(ServerConfigTestTags.Https),
            )
        }
    }
}

@Composable
private fun ServerHostField(
    host: String,
    showError: Boolean,
    onHostChanged: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    OutlinedTextField(
        value = host,
        onValueChange = onHostChanged,
        modifier = modifier.fillMaxWidth().testTag(ServerConfigTestTags.Host),
        label = { Text(stringResource(R.string.server_host_label)) },
        placeholder = { Text(stringResource(R.string.server_host_placeholder)) },
        singleLine = true,
        isError = showError,
        keyboardOptions =
        KeyboardOptions(
            keyboardType = KeyboardType.Uri,
            imeAction = ImeAction.Next,
        ),
    )
}

@Composable
private fun ServerPortField(
    port: String,
    showError: Boolean,
    onPortChanged: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    OutlinedTextField(
        value = port,
        onValueChange = onPortChanged,
        modifier = modifier.fillMaxWidth().testTag(ServerConfigTestTags.Port),
        label = { Text(stringResource(R.string.server_port_label)) },
        singleLine = true,
        isError = showError,
        supportingText =
        if (showError) {
            { Text(stringResource(R.string.server_invalid_endpoint)) }
        } else {
            null
        },
        keyboardOptions =
        KeyboardOptions(
            keyboardType = KeyboardType.Number,
            imeAction = ImeAction.Done,
        ),
    )
}

private fun parseEndpoint(host: String, port: String, useHttps: Boolean): ServerEndpoint? = ServerEndpoint.parse(
    host = host,
    port = port,
    protocol =
    if (useHttps) {
        ServerProtocol.HTTPS
    } else {
        ServerProtocol.HTTP
    },
)
