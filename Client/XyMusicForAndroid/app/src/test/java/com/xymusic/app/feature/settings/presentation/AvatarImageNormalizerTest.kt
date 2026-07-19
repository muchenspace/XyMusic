package com.xymusic.app.feature.settings.presentation

import android.app.Application
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.net.Uri
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.settings.domain.model.AvatarUploadCommand
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import org.junit.Assert.assertThrows
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class AvatarImageNormalizerTest {
    @Test
    fun largeSystemImageIsScaledAndConvertedToBoundedJpeg() {
        val source = png(width = 2_048, height = 1_024)
        val normalizer = normalizer(source)

        val command = normalizer.normalize(TEST_URI)

        assertThat(command.fileName).isEqualTo("avatar.jpg")
        assertThat(command.contentType).isEqualTo("image/jpeg")
        assertThat(command.content.size).isAtMost(AvatarUploadCommand.MAX_BYTES)
        assertThat(command.content.take(2)).containsExactly(0xFF.toByte(), 0xD8.toByte()).inOrder()
        val decoded = BitmapFactory.decodeByteArray(command.content, 0, command.content.size)
        try {
            assertThat(decoded.width).isEqualTo(1_024)
            assertThat(decoded.height).isEqualTo(512)
        } finally {
            decoded.recycle()
        }
    }

    @Test
    fun sourceLargerThanUploadLimitIsNormalizedInsteadOfRejected() {
        val png = png(width = 320, height = 240)
        val source = png + ByteArray(AvatarUploadCommand.MAX_BYTES + 1 - png.size)

        val command = normalizer(source).normalize(TEST_URI)

        assertThat(source.size).isGreaterThan(AvatarUploadCommand.MAX_BYTES)
        assertThat(command.content.size).isAtMost(AvatarUploadCommand.MAX_BYTES)
    }

    @Test
    fun corruptImageFailsExplicitly() {
        val normalizer = normalizer(byteArrayOf(0x13, 0x37, 0x42))

        assertThrows(InvalidAvatarImageException::class.java) {
            normalizer.normalize(TEST_URI)
        }
    }

    @Test
    fun unsupportedImageFailsExplicitly() {
        val svg = "<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>".encodeToByteArray()

        assertThrows(InvalidAvatarImageException::class.java) {
            normalizer(svg).normalize(TEST_URI)
        }
    }

    @Test
    fun jpegThatCannotFitConfiguredLimitReportsTooLarge() {
        val source = png(width = 64, height = 64)
        val normalizer =
            AvatarImageNormalizer(
                openInputStream = { ByteArrayInputStream(source) },
                maxOutputBytes = 32,
            )

        assertThrows(AvatarImageTooLargeException::class.java) {
            normalizer.normalize(TEST_URI)
        }
    }

    private fun normalizer(content: ByteArray) = AvatarImageNormalizer(
        openInputStream = { ByteArrayInputStream(content) },
    )

    private fun png(width: Int, height: Int): ByteArray {
        val bitmap = Bitmap.createBitmap(width, height, Bitmap.Config.ARGB_8888)
        return try {
            Canvas(bitmap).drawRect(
                0f,
                0f,
                width.toFloat(),
                height.toFloat(),
                Paint(Paint.ANTI_ALIAS_FLAG).apply { color = Color.rgb(40, 120, 220) },
            )
            ByteArrayOutputStream().use { output ->
                check(bitmap.compress(Bitmap.CompressFormat.PNG, 100, output))
                output.toByteArray()
            }
        } finally {
            bitmap.recycle()
        }
    }

    private companion object {
        val TEST_URI: Uri = Uri.parse("content://test/avatar")
    }
}
