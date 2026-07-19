package com.xymusic.app.feature.player.data.media

import android.net.Uri
import androidx.media3.common.C
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.DataSpec
import androidx.media3.datasource.TransferListener
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.preferences.MobileDataPolicy
import java.io.IOException
import org.junit.Assert.assertThrows
import org.junit.Test

@UnstableApi
class PlaybackNetworkPolicyTest {
    @Test
    fun unmeteredNetworkIsAllowedForEveryPolicy() {
        MobileDataPolicy.entries.forEach { policy ->
            assertThat(isStreamingAllowed(policy, isActiveNetworkMetered = false)).isTrue()
        }
    }

    @Test
    fun meteredNetworkRequiresExplicitStreamingPermission() {
        assertThat(
            isStreamingAllowed(
                MobileDataPolicy.ALLOW_STREAMING,
                isActiveNetworkMetered = true,
            ),
        ).isTrue()
        assertThat(
            isStreamingAllowed(
                MobileDataPolicy.WIFI_ONLY,
                isActiveNetworkMetered = true,
            ),
        ).isFalse()
    }

    @Test
    fun meteredNetworkIsDeniedUntilSettingsLoad() {
        assertThat(
            isStreamingAllowed(
                policy = null,
                isActiveNetworkMetered = true,
            ),
        ).isFalse()
        assertThat(
            isStreamingAllowed(
                policy = null,
                isActiveNetworkMetered = false,
            ),
        ).isTrue()
    }

    @Test
    fun activeTransferRechecksPolicyBeforeEveryRead() {
        var streamingAllowed = true
        val delegate = RecordingDataSource()
        val dataSource =
            PolicyEnforcingDataSource(delegate) {
                if (!streamingAllowed) throw IOException("streaming blocked")
            }
        val buffer = ByteArray(1)

        dataSource.read(buffer, 0, buffer.size)
        streamingAllowed = false

        assertThrows(IOException::class.java) {
            dataSource.read(buffer, 0, buffer.size)
        }
        assertThat(delegate.readCount).isEqualTo(1)
    }

    private class RecordingDataSource : DataSource {
        var openCount = 0
        var readCount = 0

        override fun addTransferListener(transferListener: TransferListener) = Unit

        override fun open(dataSpec: DataSpec): Long {
            openCount += 1
            return C.LENGTH_UNSET.toLong()
        }

        override fun read(buffer: ByteArray, offset: Int, length: Int): Int {
            readCount += 1
            return C.RESULT_END_OF_INPUT
        }

        override fun getUri(): Uri? = null

        override fun getResponseHeaders(): Map<String, List<String>> = emptyMap()

        override fun close() = Unit
    }
}
