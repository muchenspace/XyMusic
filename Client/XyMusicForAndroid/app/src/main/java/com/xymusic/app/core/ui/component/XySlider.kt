package com.xymusic.app.core.ui.component

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Slider
import androidx.compose.material3.SliderDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Fill
import androidx.compose.ui.unit.DpSize
import androidx.compose.ui.unit.dp

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun XySlider(
    value: Float,
    onValueChange: (Float) -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    valueRange: ClosedFloatingPointRange<Float> = 0f..1f,
    steps: Int = 0,
    onValueChangeFinished: (() -> Unit)? = null,
    compact: Boolean = false,
    thumbSize: Float = 14f,
    trackHeight: Float = 4f,
    activeColor: Color = Color.Unspecified,
    inactiveColor: Color = Color.Unspecified,
) {
    val resolvedActive = if (activeColor == Color.Unspecified) MaterialTheme.colorScheme.primary else activeColor
    val resolvedInactive =
        if (inactiveColor ==
            Color.Unspecified
        ) {
            MaterialTheme.colorScheme.surfaceContainerHighest
        } else {
            inactiveColor
        }
    val range = valueRange.endInclusive - valueRange.start
    val fraction =
        if (range > 0f) {
            ((value - valueRange.start) / range).coerceIn(0f, 1f)
        } else {
            0f
        }
    val interactionSource = remember(compact) { MutableInteractionSource() }
    Slider(
        value = value.coerceIn(valueRange.start, valueRange.endInclusive),
        onValueChange = onValueChange,
        modifier = modifier.fillMaxWidth(),
        enabled = enabled,
        valueRange = valueRange,
        steps = steps,
        onValueChangeFinished = onValueChangeFinished,
        interactionSource = interactionSource,
        colors =
        SliderDefaults.colors(
            thumbColor = resolvedActive,
            activeTrackColor = resolvedActive,
            inactiveTrackColor = resolvedInactive,
            activeTickColor = resolvedActive,
            inactiveTickColor = resolvedInactive,
        ),
        thumb = {
            SliderDefaults.Thumb(
                interactionSource = interactionSource,
                modifier = Modifier.height(thumbSize.dp),
                colors = SliderDefaults.colors(thumbColor = resolvedActive),
                enabled = enabled,
                thumbSize = DpSize(thumbSize.dp, thumbSize.dp),
            )
        },
        track = {
            Box(modifier = Modifier.height((trackHeight + 8).dp)) {
                Canvas(
                    modifier =
                    Modifier
                        .align(Alignment.Center)
                        .fillMaxWidth()
                        .height(trackHeight.dp),
                ) {
                    val trackWidth = size.width
                    val resolvedTrackHeight = size.height
                    val cornerRadius = resolvedTrackHeight / 2
                    drawRoundRect(
                        color = resolvedInactive,
                        topLeft = Offset.Zero,
                        size = Size(trackWidth, resolvedTrackHeight),
                        cornerRadius = CornerRadius(cornerRadius, cornerRadius),
                        style = Fill,
                    )
                    val progressWidth = trackWidth * fraction
                    if (progressWidth > 0f) {
                        drawRoundRect(
                            color = resolvedActive,
                            topLeft = Offset.Zero,
                            size = Size(progressWidth, resolvedTrackHeight),
                            cornerRadius = CornerRadius(cornerRadius, cornerRadius),
                            style = Fill,
                        )
                    }
                }
            }
        },
    )
}
