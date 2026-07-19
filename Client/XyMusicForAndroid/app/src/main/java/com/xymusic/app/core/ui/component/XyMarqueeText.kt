package com.xymusic.app.core.ui.component

import androidx.compose.foundation.basicMarquee
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.style.TextOverflow

/**
 * Auto-scrolling marquee text for long titles. Stays still when the text fits
 * the container; otherwise scrolls horizontally with a brief pause at each end.
 * Backed by the official Compose `basicMarquee` modifier.
 */
@Composable
fun XyMarqueeText(
    text: String,
    modifier: Modifier = Modifier,
    style: TextStyle = MaterialTheme.typography.bodyMedium,
    color: Color = MaterialTheme.colorScheme.onSurface,
    maxLines: Int = 1,
) {
    Text(
        text = text,
        style = style.copy(color = color),
        maxLines = maxLines,
        overflow = TextOverflow.Ellipsis,
        modifier =
        modifier
            .fillMaxWidth()
            .basicMarquee(),
    )
}
