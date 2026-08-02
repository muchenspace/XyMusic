package com.xymusic.app.feature.auth.presentation

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Visibility
import androidx.compose.material.icons.filled.VisibilityOff
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.painter.Painter
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import com.xymusic.app.R
import com.xymusic.app.core.ui.layout.isCompactLandscape
import com.xymusic.app.core.ui.layout.isWideLandscape
import com.xymusic.app.ui.theme.spacing

internal val LocalAuthBrandPainter = staticCompositionLocalOf<Painter?> { null }

@Composable
internal fun AuthFormScaffold(
    title: String,
    onBack: (() -> Unit)?,
    modifier: Modifier = Modifier,
    description: String? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    val colorScheme = MaterialTheme.colorScheme
    val brandPainter = LocalAuthBrandPainter.current ?: painterResource(R.drawable.xymusic_compact)
    BoxWithConstraints(
        modifier =
        modifier
            .fillMaxSize()
            .background(colorScheme.background)
            .imePadding(),
    ) {
        val wideLandscape = isWideLandscape(maxWidth, maxHeight)
        val compactLandscape = isCompactLandscape(maxWidth, maxHeight)
        if (wideLandscape) {
            if (onBack != null) {
                IconButton(
                    onClick = onBack,
                    modifier =
                    Modifier
                        .align(Alignment.TopStart)
                        .padding(MaterialTheme.spacing.small),
                ) {
                    Icon(
                        imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                        contentDescription = stringResource(R.string.common_back),
                        tint = colorScheme.onSurface,
                    )
                }
            }
            Row(
                modifier =
                Modifier
                    .align(Alignment.Center)
                    .widthIn(max = 960.dp)
                    .fillMaxWidth()
                    .fillMaxHeight()
                    .padding(
                        start = if (onBack == null) MaterialTheme.spacing.large else 64.dp,
                        end = MaterialTheme.spacing.large,
                        top = if (compactLandscape) MaterialTheme.spacing.compact else MaterialTheme.spacing.large,
                        bottom =
                        if (compactLandscape) {
                            MaterialTheme.spacing.compact
                        } else {
                            MaterialTheme.spacing.large
                        },
                    ),
                horizontalArrangement = Arrangement.spacedBy(MaterialTheme.spacing.extraLarge),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                AuthLandscapeBrand(
                    title = title,
                    description = description,
                    compact = compactLandscape,
                    brandPainter = brandPainter,
                    modifier =
                    Modifier
                        .weight(1f)
                        .fillMaxHeight()
                        .testTag(AuthTestTags.FormBrand),
                )
                Column(
                    modifier =
                    Modifier
                        .weight(1f)
                        .widthIn(max = 440.dp)
                        .fillMaxHeight()
                        .verticalScroll(rememberScrollState())
                        .padding(vertical = if (compactLandscape) 4.dp else MaterialTheme.spacing.medium)
                        .testTag(AuthTestTags.FormFields),
                    verticalArrangement =
                    Arrangement.spacedBy(
                        if (compactLandscape) {
                            MaterialTheme.spacing.small
                        } else {
                            MaterialTheme.spacing.compact
                        },
                    ),
                    content = content,
                )
            }
        } else {
            Column(
                modifier =
                Modifier
                    .fillMaxSize()
                    .verticalScroll(rememberScrollState()),
            ) {
                if (onBack != null) {
                    Row(
                        modifier =
                        Modifier
                            .fillMaxWidth()
                            .padding(MaterialTheme.spacing.small),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        IconButton(onClick = onBack) {
                            Icon(
                                imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                                contentDescription = stringResource(R.string.common_back),
                                tint = colorScheme.onSurface,
                            )
                        }
                    }
                }
                Column(
                    modifier =
                    Modifier
                        .widthIn(max = 520.dp)
                        .fillMaxWidth()
                        .padding(horizontal = MaterialTheme.spacing.large),
                    verticalArrangement = Arrangement.spacedBy(MaterialTheme.spacing.compact),
                ) {
                    Spacer(modifier = Modifier.height(if (onBack == null) 40.dp else 8.dp))
                    Image(
                        painter = brandPainter,
                        contentDescription = null,
                        modifier =
                        Modifier
                            .size(72.dp)
                            .clip(MaterialTheme.shapes.large),
                    )
                    Spacer(modifier = Modifier.height(4.dp))
                    Text(
                        text = stringResource(R.string.app_name),
                        color = colorScheme.primary,
                        style = MaterialTheme.typography.labelLarge,
                    )
                    Spacer(modifier = Modifier.height(MaterialTheme.spacing.small))
                    Text(
                        text = title,
                        style = MaterialTheme.typography.headlineLarge,
                        fontWeight = FontWeight.Bold,
                        color = colorScheme.onSurface,
                    )
                    description?.let {
                        Text(
                            text = it,
                            color = colorScheme.onSurfaceVariant,
                            style = MaterialTheme.typography.bodyMedium,
                        )
                    }
                    Spacer(modifier = Modifier.height(MaterialTheme.spacing.medium))
                    content()
                    Spacer(modifier = Modifier.height(MaterialTheme.spacing.large))
                }
            }
        }
    }
}

@Composable
private fun AuthLandscapeBrand(
    title: String,
    description: String?,
    compact: Boolean,
    brandPainter: Painter,
    modifier: Modifier = Modifier,
) {
    val colorScheme = MaterialTheme.colorScheme
    Column(
        modifier = modifier.padding(vertical = if (compact) 4.dp else MaterialTheme.spacing.medium),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.Start,
    ) {
        Image(
            painter = brandPainter,
            contentDescription = null,
            modifier =
            Modifier
                .size(if (compact) 48.dp else 72.dp)
                .clip(MaterialTheme.shapes.large),
        )
        Spacer(modifier = Modifier.height(if (compact) 4.dp else MaterialTheme.spacing.small))
        Text(
            text = stringResource(R.string.app_name),
            color = colorScheme.primary,
            style = MaterialTheme.typography.labelLarge,
        )
        Spacer(modifier = Modifier.height(if (compact) 4.dp else MaterialTheme.spacing.small))
        Text(
            text = title,
            style = if (compact) MaterialTheme.typography.headlineMedium else MaterialTheme.typography.headlineLarge,
            fontWeight = FontWeight.Bold,
            color = colorScheme.onSurface,
        )
        description?.let {
            Spacer(modifier = Modifier.height(if (compact) 2.dp else MaterialTheme.spacing.compact))
            Text(
                text = it,
                color = colorScheme.onSurfaceVariant,
                style = MaterialTheme.typography.bodyMedium,
            )
        }
    }
}

@Composable
internal fun AuthTextField(
    value: String,
    onValueChange: (String) -> Unit,
    field: AuthField,
    error: AuthFieldError?,
    enabled: Boolean,
    keyboardOptions: KeyboardOptions,
    testTag: String,
    keyboardActions: KeyboardActions = KeyboardActions.Default,
    leadingIcon: @Composable (() -> Unit)? = null,
) {
    val interactionSource = remember { MutableInteractionSource() }
    val colorScheme = MaterialTheme.colorScheme
    BasicTextField(
        value = value,
        onValueChange = onValueChange,
        modifier =
        Modifier
            .fillMaxWidth()
            .testTag(testTag),
        enabled = enabled,
        textStyle =
        LocalTextStyle.current.copy(
            color = if (enabled) colorScheme.onSurface else colorScheme.onSurfaceVariant,
        ),
        cursorBrush = SolidColor(colorScheme.primary),
        keyboardOptions = keyboardOptions,
        keyboardActions = keyboardActions,
        singleLine = true,
        interactionSource = interactionSource,
        decorationBox = { innerTextField ->
            AuthTextFieldDecoration(
                value = value,
                enabled = enabled,
                isError = error != null,
                interactionSource = interactionSource,
                label = { Text(stringResource(field.labelRes())) },
                supportingText = error?.let { { Text(authFieldErrorText(it)) } },
                leadingIcon = leadingIcon,
                innerTextField = innerTextField,
            )
        },
    )
}

@Composable
internal fun PasswordField(
    value: String,
    onValueChange: (String) -> Unit,
    field: AuthField,
    error: AuthFieldError?,
    enabled: Boolean,
    imeAction: ImeAction,
    testTag: String,
    onDone: () -> Unit = {},
    leadingIcon: @Composable (() -> Unit)? = null,
) {
    var passwordVisible by rememberSaveable { mutableStateOf(false) }
    val interactionSource = remember { MutableInteractionSource() }
    val colorScheme = MaterialTheme.colorScheme
    val visualTransformation =
        if (passwordVisible) {
            VisualTransformation.None
        } else {
            PasswordVisualTransformation()
        }
    BasicTextField(
        value = value,
        onValueChange = onValueChange,
        modifier =
        Modifier
            .fillMaxWidth()
            .testTag(testTag),
        enabled = enabled,
        textStyle =
        LocalTextStyle.current.copy(
            color = if (enabled) colorScheme.onSurface else colorScheme.onSurfaceVariant,
        ),
        cursorBrush = SolidColor(colorScheme.primary),
        keyboardOptions =
        KeyboardOptions(
            keyboardType = KeyboardType.Password,
            imeAction = imeAction,
        ),
        keyboardActions = KeyboardActions(onDone = { onDone() }),
        singleLine = true,
        visualTransformation = visualTransformation,
        interactionSource = interactionSource,
        decorationBox = { innerTextField ->
            AuthTextFieldDecoration(
                value = value,
                enabled = enabled,
                isError = error != null,
                interactionSource = interactionSource,
                visualTransformation = visualTransformation,
                label = { Text(stringResource(field.labelRes())) },
                supportingText = error?.let { { Text(authFieldErrorText(it)) } },
                leadingIcon = leadingIcon,
                trailingIcon = {
                    IconButton(
                        onClick = { passwordVisible = !passwordVisible },
                        enabled = enabled,
                    ) {
                        Icon(
                            imageVector =
                            if (passwordVisible) {
                                Icons.Default.VisibilityOff
                            } else {
                                Icons.Default.Visibility
                            },
                            contentDescription =
                            stringResource(
                                if (passwordVisible) {
                                    R.string.auth_hide_password
                                } else {
                                    R.string.auth_show_password
                                },
                            ),
                        )
                    }
                },
                innerTextField = innerTextField,
            )
        },
    )
}

@Composable
private fun AuthTextFieldDecoration(
    value: String,
    enabled: Boolean,
    isError: Boolean,
    interactionSource: MutableInteractionSource,
    innerTextField: @Composable () -> Unit,
    visualTransformation: VisualTransformation = VisualTransformation.None,
    label: @Composable (() -> Unit)? = null,
    supportingText: @Composable (() -> Unit)? = null,
    leadingIcon: @Composable (() -> Unit)? = null,
    trailingIcon: @Composable (() -> Unit)? = null,
) {
    val colorScheme = MaterialTheme.colorScheme
    val colors =
        OutlinedTextFieldDefaults.colors(
            focusedBorderColor = colorScheme.primary,
            unfocusedBorderColor = colorScheme.outline,
            cursorColor = colorScheme.primary,
            focusedLabelColor = colorScheme.primary,
            unfocusedLabelColor = colorScheme.onSurfaceVariant,
            errorBorderColor = colorScheme.error,
            errorLabelColor = colorScheme.error,
            focusedTextColor = colorScheme.onSurface,
            unfocusedTextColor = colorScheme.onSurface,
            disabledTextColor = colorScheme.onSurfaceVariant,
        )
    OutlinedTextFieldDefaults.DecorationBox(
        value = value,
        innerTextField = innerTextField,
        enabled = enabled,
        singleLine = true,
        visualTransformation = visualTransformation,
        interactionSource = interactionSource,
        isError = isError,
        label = label,
        supportingText = supportingText,
        leadingIcon = leadingIcon,
        trailingIcon = trailingIcon,
        colors = colors,
        container = {
            OutlinedTextFieldDefaults.Container(
                enabled = enabled,
                isError = isError,
                interactionSource = interactionSource,
                colors = colors,
                shape = MaterialTheme.shapes.medium,
            )
        },
    )
}

@Composable
internal fun SubmissionButton(label: String, isSubmitting: Boolean, onClick: () -> Unit) {
    Button(
        onClick = onClick,
        modifier =
        Modifier
            .fillMaxWidth()
            .height(50.dp)
            .testTag(AuthTestTags.Submit),
        enabled = !isSubmitting,
        shape = MaterialTheme.shapes.extraLarge,
        colors =
        ButtonDefaults.buttonColors(
            containerColor = MaterialTheme.colorScheme.surfaceContainerHighest,
            contentColor = MaterialTheme.colorScheme.onSurface,
        ),
    ) {
        if (isSubmitting) {
            CircularProgressIndicator(
                modifier = Modifier.size(20.dp),
                color = MaterialTheme.colorScheme.onSurface,
                strokeWidth = 2.dp,
            )
            Spacer(modifier = Modifier.width(MaterialTheme.spacing.small))
        }
        Text(
            text =
            if (isSubmitting) {
                stringResource(R.string.auth_submitting)
            } else {
                label
            },
            style = MaterialTheme.typography.labelLarge,
        )
    }
}
