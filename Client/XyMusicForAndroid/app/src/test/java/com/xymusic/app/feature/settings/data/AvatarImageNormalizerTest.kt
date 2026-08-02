package com.xymusic.app.feature.settings.data

import android.app.Application
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.settings.domain.AvatarImageSource
import com.xymusic.app.feature.settings.domain.AvatarImageTooLargeException
import com.xymusic.app.feature.settings.domain.InvalidAvatarImageException
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
        val normalizer = normalizer()

        val command = normalizer.normalize(sourceInput(source))

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
        val image = png(width = 320, height = 240)
        val source = image + ByteArray(AvatarUploadCommand.MAX_BYTES + 1 - image.size)

        val command = normalizer().normalize(sourceInput(source))

        assertThat(source.size).isGreaterThan(AvatarUploadCommand.MAX_BYTES)
        assertThat(command.content.size).isAtMost(AvatarUploadCommand.MAX_BYTES)
    }

    @Test
    fun corruptImageFailsExplicitly() {
        assertThrows(InvalidAvatarImageException::class.java) {
            normalizer().normalize(sourceInput(byteArrayOf(0x13, 0x37, 0x42)))
        }
    }

    @Test
    fun unsupportedImageFailsExplicitly() {
        val svg = "<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>".encodeToByteArray()

        assertThrows(InvalidAvatarImageException::class.java) {
            normalizer().normalize(sourceInput(svg))
        }
    }

    @Test
    fun jpegThatCannotFitConfiguredLimitReportsTooLarge() {
        val source = png(width = 64, height = 64)
        val normalizer = AvatarImageNormalizerCore(maxOutputBytes = 32)

        assertThrows(AvatarImageTooLargeException::class.java) {
            normalizer.normalize(sourceInput(source))
        }
    }

    private fun normalizer() = AvatarImageNormalizerCore()

    private fun sourceInput(content: ByteArray) = AvatarImageSource { ByteArrayInputStream(content) }

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
}
