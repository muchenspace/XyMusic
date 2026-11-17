package com.xymusic.app.core.ui.dns

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import com.xymusic.app.domain.settings.DnsSettings
import com.xymusic.app.domain.settings.isIpAddress

object DnsSettingsTestTags {
    const val Dialog = "dns_settings_dialog"
    const val CustomDnsSwitch = "dns_settings_custom_dns_switch"
    const val DnsServerInput = "dns_settings_dns_server_input"
    const val DohSwitch = "dns_settings_doh_switch"
    const val DohUrlInput = "dns_settings_doh_url_input"
    const val DisableCacheSwitch = "dns_settings_disable_cache_switch"
    const val SaveButton = "dns_settings_save_button"
    const val CancelButton = "dns_settings_cancel_button"
    const val ResetButton = "dns_settings_reset_button"
}

@Composable
fun DnsSettingsDialog(
    currentSettings: DnsSettings,
    onDismiss: () -> Unit,
    onSave: (DnsSettings) -> Unit,
    modifier: Modifier = Modifier,
) {
    var customDnsEnabled by rememberSaveable(currentSettings) {
        mutableStateOf(currentSettings.customDnsEnabled)
    }
    var dnsServer by rememberSaveable(currentSettings) {
        mutableStateOf(currentSettings.dnsServer)
    }
    var dohEnabled by rememberSaveable(currentSettings) {
        mutableStateOf(currentSettings.dohEnabled)
    }
    var dohUrl by rememberSaveable(currentSettings) {
        mutableStateOf(currentSettings.dohUrl)
    }
    var disableDnsCache by rememberSaveable(currentSettings) {
        mutableStateOf(currentSettings.disableDnsCache)
    }
    var dnsServerError by rememberSaveable { mutableStateOf<String?>(null) }
    var dohUrlError by rememberSaveable { mutableStateOf<String?>(null) }

    val invalidDnsMessage = stringResource(R.string.dns_settings_error_invalid_dns)
    val invalidDohMessage = stringResource(R.string.dns_settings_error_invalid_doh)

    val onConfirm = {
        val validation =
            validateDnsDialogInput(
                customDnsEnabled = customDnsEnabled,
                dnsServer = dnsServer,
                dohEnabled = dohEnabled,
                dohUrl = dohUrl,
                disableDnsCache = disableDnsCache,
                invalidDnsMessage = invalidDnsMessage,
                invalidDohMessage = invalidDohMessage,
            )
        dnsServerError = validation.dnsServerError
        dohUrlError = validation.dohUrlError
        validation.settings?.let(onSave)
    }

    AlertDialog(
        onDismissRequest = onDismiss,
        modifier = modifier.testTag(DnsSettingsTestTags.Dialog),
        title = {
            Text(
                text = stringResource(R.string.dns_settings_title),
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.Bold,
            )
        },
        text = {
            DnsSettingsDialogContent(
                state =
                DnsDialogFormState(
                    customDnsEnabled = customDnsEnabled,
                    dnsServer = dnsServer,
                    dohEnabled = dohEnabled,
                    dohUrl = dohUrl,
                    disableDnsCache = disableDnsCache,
                    dnsServerError = dnsServerError,
                    dohUrlError = dohUrlError,
                ),
                onCustomDnsChanged = {
                    customDnsEnabled = it
                    dnsServerError = null
                },
                onDnsServerChanged = {
                    dnsServer = it
                    dnsServerError = null
                },
                onDohChanged = {
                    dohEnabled = it
                    dohUrlError = null
                },
                onDohUrlChanged = {
                    dohUrl = it
                    dohUrlError = null
                },
                onDisableCacheChanged = { disableDnsCache = it },
                onReset = {
                    customDnsEnabled = true
                    dnsServer = DnsSettings.DEFAULT_DNS_SERVER
                    dohEnabled = true
                    dohUrl = DnsSettings.DEFAULT_DOH_URL
                    disableDnsCache = false
                    dnsServerError = null
                    dohUrlError = null
                },
            )
        },
        confirmButton = {
            Button(
                onClick = { onConfirm() },
                modifier = Modifier.testTag(DnsSettingsTestTags.SaveButton),
            ) {
                Text(stringResource(R.string.common_confirm))
            }
        },
        dismissButton = {
            TextButton(
                onClick = onDismiss,
                modifier = Modifier.testTag(DnsSettingsTestTags.CancelButton),
            ) {
                Text(stringResource(R.string.common_cancel))
            }
        },
    )
}

private data class DnsDialogFormState(
    val customDnsEnabled: Boolean,
    val dnsServer: String,
    val dohEnabled: Boolean,
    val dohUrl: String,
    val disableDnsCache: Boolean,
    val dnsServerError: String?,
    val dohUrlError: String?,
)

private data class DnsDialogValidation(
    val settings: DnsSettings? = null,
    val dnsServerError: String? = null,
    val dohUrlError: String? = null,
)

private fun validateDnsDialogInput(
    customDnsEnabled: Boolean,
    dnsServer: String,
    dohEnabled: Boolean,
    dohUrl: String,
    disableDnsCache: Boolean,
    invalidDnsMessage: String,
    invalidDohMessage: String,
): DnsDialogValidation {
    val trimmedDns = dnsServer.trim()
    val trimmedDoh = dohUrl.trim()
    val dnsError = validateCustomDnsInput(customDnsEnabled, trimmedDns, invalidDnsMessage)
    val dohError = validateDohUrlInput(customDnsEnabled, dohEnabled, trimmedDoh, invalidDohMessage)
    if (dnsError != null || dohError != null) {
        return DnsDialogValidation(dnsServerError = dnsError, dohUrlError = dohError)
    }
    return DnsDialogValidation(
        settings =
        DnsSettings(
            customDnsEnabled = customDnsEnabled,
            dnsServer = trimmedDns.ifBlank { DnsSettings.DEFAULT_DNS_SERVER },
            dohEnabled = dohEnabled,
            dohUrl = trimmedDoh.ifBlank { DnsSettings.DEFAULT_DOH_URL },
            disableDnsCache = disableDnsCache,
        ),
    )
}

private fun validateCustomDnsInput(customDnsEnabled: Boolean, trimmedDns: String, invalidDnsMessage: String): String? {
    if (!customDnsEnabled) return null
    val ips = trimmedDns.split(',', ';', ' ').map(String::trim).filter(String::isNotEmpty)
    return if (ips.isEmpty() || !ips.all(::isIpAddress)) invalidDnsMessage else null
}

private fun validateDohUrlInput(
    customDnsEnabled: Boolean,
    dohEnabled: Boolean,
    trimmedDoh: String,
    invalidDohMessage: String,
): String? {
    if (!customDnsEnabled || !dohEnabled) return null
    return if (trimmedDoh.isBlank() || !trimmedDoh.startsWith("https://", ignoreCase = true)) {
        invalidDohMessage
    } else {
        null
    }
}

@Composable
private fun DnsSettingsDialogContent(
    state: DnsDialogFormState,
    onCustomDnsChanged: (Boolean) -> Unit,
    onDnsServerChanged: (String) -> Unit,
    onDohChanged: (Boolean) -> Unit,
    onDohUrlChanged: (String) -> Unit,
    onDisableCacheChanged: (Boolean) -> Unit,
    onReset: () -> Unit,
) {
    Column(
        modifier =
        Modifier
            .fillMaxWidth()
            .verticalScroll(rememberScrollState()),
    ) {
        DnsSettingToggleRow(
            title = stringResource(R.string.dns_settings_custom_dns),
            summary = stringResource(R.string.dns_settings_custom_dns_summary),
            checked = state.customDnsEnabled,
            onCheckedChange = onCustomDnsChanged,
            testTag = DnsSettingsTestTags.CustomDnsSwitch,
        )
        DnsValidatedTextFieldSection(
            visible = state.customDnsEnabled,
            value = state.dnsServer,
            onValueChange = onDnsServerChanged,
            label = stringResource(R.string.dns_settings_server_label),
            placeholder = stringResource(R.string.dns_settings_server_placeholder),
            supporting = stringResource(R.string.dns_settings_server_supporting),
            error = state.dnsServerError,
            keyboardOptions =
            KeyboardOptions(
                keyboardType = KeyboardType.Ascii,
                imeAction = ImeAction.Next,
            ),
            testTag = DnsSettingsTestTags.DnsServerInput,
        )
        Spacer(modifier = Modifier.height(12.dp))
        DnsSettingToggleRow(
            title = stringResource(R.string.dns_settings_doh),
            summary = stringResource(R.string.dns_settings_doh_summary),
            checked = state.dohEnabled,
            enabled = state.customDnsEnabled,
            onCheckedChange = onDohChanged,
            testTag = DnsSettingsTestTags.DohSwitch,
        )
        DnsValidatedTextFieldSection(
            visible = state.customDnsEnabled && state.dohEnabled,
            value = state.dohUrl,
            onValueChange = onDohUrlChanged,
            label = stringResource(R.string.dns_settings_doh_url_label),
            placeholder = stringResource(R.string.dns_settings_doh_url_placeholder),
            supporting = stringResource(R.string.dns_settings_doh_url_supporting),
            error = state.dohUrlError,
            keyboardOptions =
            KeyboardOptions(
                keyboardType = KeyboardType.Uri,
                imeAction = ImeAction.Done,
            ),
            testTag = DnsSettingsTestTags.DohUrlInput,
        )
        Spacer(modifier = Modifier.height(12.dp))
        DnsSettingToggleRow(
            title = stringResource(R.string.dns_settings_disable_cache),
            summary = stringResource(R.string.dns_settings_disable_cache_summary),
            checked = state.disableDnsCache,
            onCheckedChange = onDisableCacheChanged,
            testTag = DnsSettingsTestTags.DisableCacheSwitch,
        )
        Spacer(modifier = Modifier.height(8.dp))
        TextButton(
            onClick = onReset,
            modifier = Modifier.testTag(DnsSettingsTestTags.ResetButton),
        ) {
            Text(stringResource(R.string.dns_settings_reset))
        }
    }
}

@Composable
private fun DnsValidatedTextFieldSection(
    visible: Boolean,
    value: String,
    onValueChange: (String) -> Unit,
    label: String,
    placeholder: String,
    supporting: String,
    error: String?,
    keyboardOptions: KeyboardOptions,
    testTag: String,
) {
    AnimatedVisibility(visible = visible) {
        Column(modifier = Modifier.padding(top = 8.dp, bottom = 4.dp)) {
            OutlinedTextField(
                value = value,
                onValueChange = onValueChange,
                label = { Text(label) },
                placeholder = { Text(placeholder) },
                supportingText = {
                    Text(
                        text = error ?: supporting,
                        color =
                        if (error != null) {
                            MaterialTheme.colorScheme.error
                        } else {
                            MaterialTheme.colorScheme.onSurfaceVariant
                        },
                    )
                },
                isError = error != null,
                singleLine = true,
                keyboardOptions = keyboardOptions,
                modifier =
                Modifier
                    .fillMaxWidth()
                    .testTag(testTag),
            )
        }
    }
}

@Composable
private fun DnsSettingToggleRow(
    title: String,
    summary: String,
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
    testTag: String,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    Row(
        modifier =
        modifier
            .fillMaxWidth()
            .toggleable(
                value = checked,
                enabled = enabled,
                role = Role.Switch,
                onValueChange = onCheckedChange,
            ).testTag(testTag)
            .padding(vertical = 6.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = title,
                style = MaterialTheme.typography.bodyLarge,
                fontWeight = FontWeight.Medium,
                color =
                if (enabled) {
                    MaterialTheme.colorScheme.onSurface
                } else {
                    MaterialTheme.colorScheme.onSurface.copy(alpha = 0.38f)
                },
            )
            Text(
                text = summary,
                style = MaterialTheme.typography.bodySmall,
                color =
                if (enabled) {
                    MaterialTheme.colorScheme.onSurfaceVariant
                } else {
                    MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.38f)
                },
            )
        }
        Spacer(modifier = Modifier.width(16.dp))
        Switch(
            checked = checked,
            onCheckedChange = null,
            enabled = enabled,
        )
    }
}
