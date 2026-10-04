package com.xymusic.app.core.ui.dns

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.clickable
import androidx.compose.foundation.selection.toggleable
import androidx.compose.ui.semantics.Role
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
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
        var hasError = false
        val trimmedDns = dnsServer.trim()
        val trimmedDoh = dohUrl.trim()

        if (customDnsEnabled) {
            val ips = trimmedDns.split(',', ';', ' ').map { it.trim() }.filter { it.isNotEmpty() }
            if (ips.isEmpty() || !ips.all { isIpAddress(it) }) {
                dnsServerError = invalidDnsMessage
                hasError = true
            } else {
                dnsServerError = null
            }

            if (dohEnabled) {
                if (trimmedDoh.isBlank() || !trimmedDoh.startsWith("https://", ignoreCase = true)) {
                    dohUrlError = invalidDohMessage
                    hasError = true
                } else {
                    dohUrlError = null
                }
            } else {
                dohUrlError = null
            }
        } else {
            dnsServerError = null
            dohUrlError = null
        }

        if (!hasError) {
            onSave(
                DnsSettings(
                    customDnsEnabled = customDnsEnabled,
                    dnsServer = if (trimmedDns.isBlank()) DnsSettings.DEFAULT_DNS_SERVER else trimmedDns,
                    dohEnabled = dohEnabled,
                    dohUrl = if (trimmedDoh.isBlank()) DnsSettings.DEFAULT_DOH_URL else trimmedDoh,
                    disableDnsCache = disableDnsCache,
                ),
            )
        }
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
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .verticalScroll(rememberScrollState()),
            ) {
                // Custom DNS Toggle Row
                DnsSettingToggleRow(
                    title = stringResource(R.string.dns_settings_custom_dns),
                    summary = stringResource(R.string.dns_settings_custom_dns_summary),
                    checked = customDnsEnabled,
                    onCheckedChange = {
                        customDnsEnabled = it
                        dnsServerError = null
                    },
                    testTag = DnsSettingsTestTags.CustomDnsSwitch,
                )

                // DNS Server Input Field
                AnimatedVisibility(visible = customDnsEnabled) {
                    Column(modifier = Modifier.padding(top = 8.dp, bottom = 4.dp)) {
                        OutlinedTextField(
                            value = dnsServer,
                            onValueChange = {
                                dnsServer = it
                                dnsServerError = null
                            },
                            label = { Text(stringResource(R.string.dns_settings_server_label)) },
                            placeholder = { Text(stringResource(R.string.dns_settings_server_placeholder)) },
                            supportingText = {
                                Text(
                                    text = dnsServerError ?: stringResource(R.string.dns_settings_server_supporting),
                                    color = if (dnsServerError != null) {
                                        MaterialTheme.colorScheme.error
                                    } else {
                                        MaterialTheme.colorScheme.onSurfaceVariant
                                    },
                                )
                            },
                            isError = dnsServerError != null,
                            singleLine = true,
                            keyboardOptions = KeyboardOptions(
                                keyboardType = KeyboardType.Ascii,
                                imeAction = ImeAction.Next,
                            ),
                            modifier = Modifier
                                .fillMaxWidth()
                                .testTag(DnsSettingsTestTags.DnsServerInput),
                        )
                    }
                }

                Spacer(modifier = Modifier.height(12.dp))

                // DoH Toggle Row
                DnsSettingToggleRow(
                    title = stringResource(R.string.dns_settings_doh),
                    summary = stringResource(R.string.dns_settings_doh_summary),
                    checked = dohEnabled,
                    enabled = customDnsEnabled,
                    onCheckedChange = {
                        dohEnabled = it
                        dohUrlError = null
                    },
                    testTag = DnsSettingsTestTags.DohSwitch,
                )

                // DoH Server URL Input Field
                AnimatedVisibility(visible = customDnsEnabled && dohEnabled) {
                    Column(modifier = Modifier.padding(top = 8.dp, bottom = 4.dp)) {
                        OutlinedTextField(
                            value = dohUrl,
                            onValueChange = {
                                dohUrl = it
                                dohUrlError = null
                            },
                            label = { Text(stringResource(R.string.dns_settings_doh_url_label)) },
                            placeholder = { Text(stringResource(R.string.dns_settings_doh_url_placeholder)) },
                            supportingText = {
                                Text(
                                    text = dohUrlError ?: stringResource(R.string.dns_settings_doh_url_supporting),
                                    color = if (dohUrlError != null) {
                                        MaterialTheme.colorScheme.error
                                    } else {
                                        MaterialTheme.colorScheme.onSurfaceVariant
                                    },
                                )
                            },
                            isError = dohUrlError != null,
                            singleLine = true,
                            keyboardOptions = KeyboardOptions(
                                keyboardType = KeyboardType.Uri,
                                imeAction = ImeAction.Done,
                            ),
                            modifier = Modifier
                                .fillMaxWidth()
                                .testTag(DnsSettingsTestTags.DohUrlInput),
                        )
                    }
                }

                Spacer(modifier = Modifier.height(12.dp))

                // Disable DNS Cache Toggle Row
                DnsSettingToggleRow(
                    title = stringResource(R.string.dns_settings_disable_cache),
                    summary = stringResource(R.string.dns_settings_disable_cache_summary),
                    checked = disableDnsCache,
                    onCheckedChange = { disableDnsCache = it },
                    testTag = DnsSettingsTestTags.DisableCacheSwitch,
                )

                Spacer(modifier = Modifier.height(8.dp))

                // Reset to Default Button
                TextButton(
                    onClick = {
                        customDnsEnabled = true
                        dnsServer = DnsSettings.DEFAULT_DNS_SERVER
                        dohEnabled = true
                        dohUrl = DnsSettings.DEFAULT_DOH_URL
                        disableDnsCache = false
                        dnsServerError = null
                        dohUrlError = null
                    },
                    modifier = Modifier.testTag(DnsSettingsTestTags.ResetButton),
                ) {
                    Text(stringResource(R.string.dns_settings_reset))
                }
            }
        },
        confirmButton = {
            Button(
                onClick = onConfirm,
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
        modifier = modifier
            .fillMaxWidth()
            .toggleable(
                value = checked,
                enabled = enabled,
                role = Role.Switch,
                onValueChange = onCheckedChange,
            )
            .testTag(testTag)
            .padding(vertical = 6.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = title,
                style = MaterialTheme.typography.bodyLarge,
                fontWeight = FontWeight.Medium,
                color = if (enabled) {
                    MaterialTheme.colorScheme.onSurface
                } else {
                    MaterialTheme.colorScheme.onSurface.copy(alpha = 0.38f)
                },
            )
            Text(
                text = summary,
                style = MaterialTheme.typography.bodySmall,
                color = if (enabled) {
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
