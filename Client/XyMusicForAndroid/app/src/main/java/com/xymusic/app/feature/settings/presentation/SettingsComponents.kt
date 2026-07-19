package com.xymusic.app.feature.settings.presentation

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.outlined.AccountCircle
import androidx.compose.material.icons.outlined.Cached
import androidx.compose.material.icons.outlined.ChevronRight
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import com.xymusic.app.core.preferences.StreamingQuality
import com.xymusic.app.core.preferences.ThemePreference
import com.xymusic.app.core.ui.component.MediaArtwork
import com.xymusic.app.core.ui.component.XySlider
import com.xymusic.app.feature.settings.domain.model.UserProfile
import kotlin.math.roundToInt

internal enum class SettingsRowPosition {
    Single,
    First,
    Middle,
    Last,
}

@Composable
internal fun ProfileSection(profile: UserProfile?, onEdit: () -> Unit, onAvatarClick: () -> Unit) {
    Surface(
        modifier =
        Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp),
        shape = RoundedCornerShape(8.dp),
        color = MaterialTheme.colorScheme.surfaceContainerLowest,
        tonalElevation = 0.dp,
        shadowElevation = 0.dp,
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 12.dp),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                modifier =
                Modifier
                    .size(60.dp)
                    .clickable(
                        enabled = profile != null,
                        role = Role.Button,
                        onClick = onAvatarClick,
                    ),
            ) {
                MediaArtwork(
                    url = profile?.avatar?.url,
                    cacheKey = profile?.avatar?.cacheKey,
                    contentDescription = stringResource(R.string.settings_change_avatar),
                    fallbackIcon = Icons.Outlined.AccountCircle,
                    shape = CircleShape,
                    modifier = Modifier.fillMaxSize(),
                )
                Box(
                    modifier =
                    Modifier
                        .align(Alignment.BottomEnd)
                        .size(22.dp)
                        .clip(CircleShape)
                        .background(MaterialTheme.colorScheme.surfaceContainerHighest),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        imageVector = Icons.Default.Edit,
                        contentDescription = null,
                        tint = MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.size(12.dp),
                    )
                }
            }
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = profile?.displayName ?: stringResource(R.string.settings_profile_loading),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    style = MaterialTheme.typography.titleLarge,
                    fontWeight = FontWeight.SemiBold,
                )
                profile?.let {
                    it.bio?.takeIf(String::isNotBlank)?.let { bio ->
                        Text(
                            text = bio,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            style = MaterialTheme.typography.bodySmall,
                        )
                    }
                }
            }
            IconButton(
                onClick = onEdit,
                enabled = profile != null,
                modifier = Modifier.size(36.dp),
            ) {
                Icon(
                    imageVector = Icons.Outlined.ChevronRight,
                    contentDescription = stringResource(R.string.settings_edit_profile),
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
internal fun <T> OptionRow(
    icon: ImageVector,
    title: String,
    selected: T,
    options: List<T>,
    optionLabel: @Composable (T) -> String,
    onSelected: (T) -> Unit,
    position: SettingsRowPosition = SettingsRowPosition.Single,
) {
    var expanded by remember { mutableStateOf(false) }
    Box {
        SettingsRow(
            icon = icon,
            title = title,
            position = position,
            onClick = { expanded = true },
            trailing = {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = optionLabel(selected),
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        style = MaterialTheme.typography.bodyMedium,
                    )
                    Icon(
                        imageVector = Icons.Outlined.ChevronRight,
                        contentDescription = null,
                        tint = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.65f),
                        modifier = Modifier.size(18.dp),
                    )
                }
            },
        )
        DropdownMenu(
            expanded = expanded,
            onDismissRequest = { expanded = false },
        ) {
            options.forEach { option ->
                DropdownMenuItem(
                    text = { Text(optionLabel(option)) },
                    onClick = {
                        expanded = false
                        onSelected(option)
                    },
                )
            }
        }
    }
}

@Composable
internal fun SettingsToggleItem(
    icon: ImageVector,
    title: String,
    summary: String,
    checked: Boolean,
    enabled: Boolean = true,
    onCheckedChange: (Boolean) -> Unit,
    position: SettingsRowPosition = SettingsRowPosition.Single,
) {
    SettingsRow(
        icon = icon,
        title = title,
        summary = summary,
        position = position,
        enabled = enabled,
        role = Role.Switch,
        onClick = { onCheckedChange(!checked) },
        trailing = {
            Switch(
                checked = checked,
                onCheckedChange = null,
                enabled = enabled,
                colors =
                SwitchDefaults.colors(
                    checkedThumbColor = MaterialTheme.colorScheme.onSurface,
                    checkedTrackColor = MaterialTheme.colorScheme.surfaceContainerHighest,
                    uncheckedThumbColor = MaterialTheme.colorScheme.onSurfaceVariant,
                    uncheckedTrackColor = MaterialTheme.colorScheme.surfaceContainerHigh,
                ),
            )
        },
    )
}

@Composable
internal fun SettingsActionItem(
    icon: ImageVector,
    title: String,
    summary: String? = null,
    titleColor: Color = MaterialTheme.colorScheme.onSurface,
    iconTint: Color = MaterialTheme.colorScheme.onSurface,
    onClick: () -> Unit,
    position: SettingsRowPosition = SettingsRowPosition.Single,
    showChevron: Boolean = true,
) {
    SettingsRow(
        icon = icon,
        title = title,
        summary = summary,
        titleColor = titleColor,
        iconTint = iconTint,
        position = position,
        onClick = onClick,
        trailing =
        if (showChevron) {
            {
                Icon(
                    imageVector = Icons.Outlined.ChevronRight,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.65f),
                    modifier = Modifier.size(18.dp),
                )
            }
        } else {
            null
        },
    )
}

@Composable
internal fun CacheLimitRow(
    valueMiB: Int,
    onValueChanged: (Int) -> Unit,
    position: SettingsRowPosition = SettingsRowPosition.Single,
) {
    var sliderValue by remember(valueMiB) { mutableFloatStateOf(valueMiB.toFloat()) }
    SettingsRowContainer(position = position) {
        Column(
            modifier =
            Modifier
                .fillMaxWidth()
                .padding(horizontal = 16.dp, vertical = 12.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                SettingsIcon(Icons.Outlined.Cached, MaterialTheme.colorScheme.onSurface)
                Spacer(modifier = Modifier.size(12.dp))
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = stringResource(R.string.settings_cache_limit),
                        style = MaterialTheme.typography.bodyLarge,
                    )
                    Text(
                        text =
                        stringResource(
                            R.string.settings_cache_limit_value,
                            sliderValue.roundToInt(),
                        ),
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        style = MaterialTheme.typography.bodySmall,
                    )
                }
            }
            XySlider(
                value = sliderValue,
                onValueChange = { sliderValue = it },
                onValueChangeFinished = { onValueChanged(sliderValue.roundToInt()) },
                valueRange = 128f..4_096f,
                steps = 30,
            )
        }
    }
}

@Composable
internal fun SettingsHeading(text: String) {
    Text(
        text = text,
        modifier =
        Modifier.padding(
            start = 32.dp,
            end = 24.dp,
            top = 16.dp,
            bottom = 6.dp,
        ),
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        style = MaterialTheme.typography.labelLarge,
        fontWeight = FontWeight.SemiBold,
    )
}

@Composable
private fun SettingsRow(
    icon: ImageVector,
    title: String,
    position: SettingsRowPosition,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    summary: String? = null,
    titleColor: Color = MaterialTheme.colorScheme.onSurface,
    iconTint: Color = MaterialTheme.colorScheme.onSurface,
    enabled: Boolean = true,
    role: Role = Role.Button,
    trailing: (@Composable () -> Unit)? = null,
) {
    SettingsRowContainer(position = position, modifier = modifier) {
        Column {
            Row(
                modifier =
                Modifier
                    .fillMaxWidth()
                    .heightIn(min = if (summary == null) 50.dp else 62.dp)
                    .clickable(enabled = enabled, role = role, onClick = onClick)
                    .padding(horizontal = 16.dp, vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                SettingsIcon(
                    icon = icon,
                    tint = if (enabled) iconTint else MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Spacer(modifier = Modifier.size(12.dp))
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = title,
                        color = if (enabled) titleColor else MaterialTheme.colorScheme.onSurfaceVariant,
                        style = MaterialTheme.typography.bodyLarge,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    summary?.let {
                        Text(
                            text = it,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            style = MaterialTheme.typography.bodySmall,
                            maxLines = 2,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                }
                trailing?.invoke()
            }
            if (position == SettingsRowPosition.First || position == SettingsRowPosition.Middle) {
                HorizontalDivider(
                    modifier = Modifier.padding(start = 56.dp),
                    thickness = 0.5.dp,
                    color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.75f),
                )
            }
        }
    }
}

@Composable
private fun SettingsRowContainer(
    position: SettingsRowPosition,
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    Surface(
        modifier =
        modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp),
        shape = settingsRowShape(position),
        color = MaterialTheme.colorScheme.surfaceContainerLowest,
        tonalElevation = 0.dp,
        shadowElevation = 0.dp,
        content = content,
    )
}

@Composable
private fun SettingsIcon(icon: ImageVector, tint: Color) {
    val backgroundColor =
        if (tint == MaterialTheme.colorScheme.error) {
            MaterialTheme.colorScheme.errorContainer
        } else {
            MaterialTheme.colorScheme.surfaceContainerHighest
        }
    Box(
        modifier =
        Modifier
            .size(30.dp)
            .clip(RoundedCornerShape(7.dp))
            .background(backgroundColor),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = icon,
            contentDescription = null,
            tint = tint,
            modifier = Modifier.size(17.dp),
        )
    }
}

private fun settingsRowShape(position: SettingsRowPosition): Shape = when (position) {
    SettingsRowPosition.Single -> RoundedCornerShape(8.dp)
    SettingsRowPosition.First -> RoundedCornerShape(topStart = 8.dp, topEnd = 8.dp)
    SettingsRowPosition.Middle -> RoundedCornerShape(0.dp)
    SettingsRowPosition.Last -> RoundedCornerShape(bottomStart = 8.dp, bottomEnd = 8.dp)
}

internal fun ThemePreference.labelRes(): Int = when (this) {
    ThemePreference.SYSTEM -> R.string.settings_theme_system
    ThemePreference.LIGHT -> R.string.settings_theme_light
    ThemePreference.DARK -> R.string.settings_theme_dark
    ThemePreference.PEACH_PINK -> R.string.settings_theme_peach
    ThemePreference.OCEAN_BLUE -> R.string.settings_theme_ocean
    ThemePreference.TWILIGHT_PURPLE -> R.string.settings_theme_twilight
}

internal fun StreamingQuality.labelRes(): Int = when (this) {
    StreamingQuality.AUTO -> R.string.settings_quality_auto
    StreamingQuality.DATA_SAVER -> R.string.settings_quality_data_saver
    StreamingQuality.STANDARD -> R.string.settings_quality_standard
    StreamingQuality.HIGH -> R.string.settings_quality_high
    StreamingQuality.LOSSLESS -> R.string.settings_quality_lossless
}
