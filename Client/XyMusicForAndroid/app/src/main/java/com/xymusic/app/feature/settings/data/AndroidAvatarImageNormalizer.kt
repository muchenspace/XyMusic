package com.xymusic.app.feature.settings.data

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import androidx.core.graphics.createBitmap
import androidx.core.graphics.scale
import com.xymusic.app.feature.settings.domain.AvatarImageException
import com.xymusic.app.feature.settings.domain.AvatarImageNormalizer
import com.xymusic.app.feature.settings.domain.AvatarImageSource
import com.xymusic.app.feature.settings.domain.AvatarImageTooLargeException
import com.xymusic.app.feature.settings.domain.InvalidAvatarImageException
import com.xymusic.app.feature.settings.domain.model.AvatarUploadCommand
import java.io.ByteArrayOutputStream
import java.io.InputStream
import java.util.concurrent.CancellationException
import javax.inject.Inject
import kotlin.math.max
import kotlin.math.roundToInt

class AndroidAvatarImageNormalizer @Inject constructor() : AvatarImageNormalizer {
    private val normalizer = AvatarImageNormalizerCore()

    override fun normalize(source: AvatarImageSource): AvatarUploadCommand = normalizer.normalize(source)
}

internal class AvatarImageNormalizerCore(private val maxOutputBytes: Int = AvatarUploadCommand.MAX_BYTES) {
    init {
        require(maxOutputBytes in 1..AvatarUploadCommand.MAX_BYTES)
    }

    fun normalize(source: AvatarImageSource): AvatarUploadCommand = try {
        normalizeImage(source)
    } catch (error: CancellationException) {
        throw error
    } catch (error: AvatarImageException) {
        throw error
    } catch (error: OutOfMemoryError) {
        throw AvatarImageTooLargeException(error)
    } catch (error: Exception) {
        throw InvalidAvatarImageException(error)
    }

    private fun normalizeImage(source: AvatarImageSource): AvatarUploadCommand {
        val bounds = readBounds(source)
        val decoded = decode(source, bounds)
        var scaled: Bitmap? = null
        var jpegBitmap: Bitmap? = null
        try {
            scaled = scaleDown(decoded)
            jpegBitmap = flattenForJpeg(scaled)
            val content = encodeJpeg(jpegBitmap)
            return AvatarUploadCommand(
                fileName = OUTPUT_FILE_NAME,
                contentType = OUTPUT_CONTENT_TYPE,
                content = content,
            )
        } catch (error: OutOfMemoryError) {
            throw AvatarImageTooLargeException(error)
        } finally {
            jpegBitmap?.recycle()
            scaled?.takeIf { it !== decoded }?.recycle()
            decoded.recycle()
        }
    }

    private fun readBounds(source: AvatarImageSource): ImageBounds {
        val options = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        open(source).use { input -> BitmapFactory.decodeStream(input, null, options) }
        if (options.outWidth <= 0 || options.outHeight <= 0) {
            throw InvalidAvatarImageException()
        }
        return ImageBounds(options.outWidth, options.outHeight)
    }

    private fun decode(source: AvatarImageSource, bounds: ImageBounds): Bitmap {
        val options =
            BitmapFactory.Options().apply {
                inPreferredConfig = Bitmap.Config.ARGB_8888
                inSampleSize = sampleSize(bounds)
            }
        return try {
            open(source).use { input ->
                BitmapFactory.decodeStream(input, null, options)
                    ?: throw InvalidAvatarImageException()
            }
        } catch (error: OutOfMemoryError) {
            throw AvatarImageTooLargeException(error)
        }
    }

    private fun open(source: AvatarImageSource): InputStream = try {
        source.openInputStream() ?: throw InvalidAvatarImageException()
    } catch (error: CancellationException) {
        throw error
    } catch (error: AvatarImageException) {
        throw error
    } catch (error: Exception) {
        throw InvalidAvatarImageException(error)
    }

    private fun sampleSize(bounds: ImageBounds): Int {
        val longestEdge = max(bounds.width, bounds.height)
        var sampleSize = 1
        while (longestEdge / sampleSize > MAX_DECODE_EDGE_PX && sampleSize <= Int.MAX_VALUE / 2) {
            sampleSize *= 2
        }
        return sampleSize
    }

    private fun scaleDown(bitmap: Bitmap): Bitmap {
        val longestEdge = max(bitmap.width, bitmap.height)
        if (longestEdge <= MAX_EDGE_PX) return bitmap
        val scale = MAX_EDGE_PX.toFloat() / longestEdge
        val width = (bitmap.width * scale).roundToInt().coerceAtLeast(1)
        val height = (bitmap.height * scale).roundToInt().coerceAtLeast(1)
        return bitmap.scale(width, height)
    }

    private fun flattenForJpeg(bitmap: Bitmap): Bitmap {
        val output = createBitmap(bitmap.width, bitmap.height)
        Canvas(output).apply {
            drawColor(Color.WHITE)
            drawBitmap(bitmap, 0f, 0f, Paint(Paint.ANTI_ALIAS_FLAG or Paint.FILTER_BITMAP_FLAG))
        }
        return output
    }

    private fun encodeJpeg(bitmap: Bitmap): ByteArray {
        val output = ByteArrayOutputStream()
        for (quality in JPEG_QUALITIES) {
            output.reset()
            if (!bitmap.compress(Bitmap.CompressFormat.JPEG, quality, output)) {
                throw InvalidAvatarImageException()
            }
            if (output.size() in 1..maxOutputBytes) return output.toByteArray()
        }
        throw AvatarImageTooLargeException()
    }

    private data class ImageBounds(val width: Int, val height: Int)

    private companion object {
        const val MAX_EDGE_PX = 1_024
        const val MAX_DECODE_EDGE_PX = MAX_EDGE_PX * 2
        const val OUTPUT_FILE_NAME = "avatar.jpg"
        const val OUTPUT_CONTENT_TYPE = "image/jpeg"
        val JPEG_QUALITIES = intArrayOf(90, 80, 70, 60, 50)
    }
}
