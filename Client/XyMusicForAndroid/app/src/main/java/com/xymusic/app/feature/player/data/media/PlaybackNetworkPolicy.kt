package com.xymusic.app.feature.player.data.media

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.DataSpec
import androidx.media3.datasource.TransferListener
import com.xymusic.app.core.common.IoDispatcher
import com.xymusic.app.domain.settings.AppSettingsRepository
import com.xymusic.app.domain.settings.MobileDataPolicy
import com.xymusic.app.feature.player.domain.AutomaticPlaybackQualityPolicy
import dagger.hilt.android.qualifiers.ApplicationContext
import java.io.IOException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch

@Singleton
class PlaybackNetworkPolicy
@Inject
constructor(
    @ApplicationContext context: Context,
    settingsRepository: AppSettingsRepository,
    @IoDispatcher ioDispatcher: CoroutineDispatcher,
    private val automaticQualityController: AutomaticPlaybackQualityPolicy,
) {
    private val connectivityManager =
        context.getSystemService(ConnectivityManager::class.java)
    private val scope = CoroutineScope(SupervisorJob() + ioDispatcher)

    @Volatile
    private var activeNetworkState: ActiveNetworkState? = null

    @Volatile
    private var mobileDataPolicy: MobileDataPolicy? = null

    private val networkCallback =
        object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                val previousNetwork = activeNetworkState?.network
                activeNetworkState = connectivityManager.snapshotNetwork(network)
                if (previousNetwork != null && previousNetwork != network) {
                    automaticQualityController.resetNetworkEstimate()
                }
            }

            override fun onCapabilitiesChanged(network: Network, networkCapabilities: NetworkCapabilities) {
                if (activeNetworkState?.network == network) {
                    activeNetworkState =
                        ActiveNetworkState(
                            network = network,
                            isMetered = networkCapabilities.isMeteredOrUnknown(),
                        )
                }
            }

            override fun onLost(network: Network) {
                if (activeNetworkState?.network == network) {
                    activeNetworkState = ActiveNetworkState(network = null, isMetered = true)
                    automaticQualityController.resetNetworkEstimate()
                }
            }
        }

    init {
        connectivityManager.registerDefaultNetworkCallback(networkCallback)
        scope.launch {
            settingsRepository.settings
                .distinctUntilChanged()
                .collect { settings ->
                    mobileDataPolicy = settings.mobileDataPolicy
                    automaticQualityController.updateStreamingPreference(settings.streamingQuality)
                }
        }
    }

    fun requireStreamingAllowed() {
        val isActiveNetworkMetered =
            activeNetworkState?.isMetered
                ?: connectivityManager.isActiveNetworkMetered
        if (!isStreamingAllowed(mobileDataPolicy, isActiveNetworkMetered)) {
            throw IOException("Streaming on the active network is disabled by user settings")
        }
    }
}

private data class ActiveNetworkState(val network: Network?, val isMetered: Boolean)

private fun ConnectivityManager.snapshotNetwork(network: Network): ActiveNetworkState = ActiveNetworkState(
    network = network,
    isMetered = getNetworkCapabilities(network).isMeteredOrUnknown(),
)

private fun NetworkCapabilities?.isMeteredOrUnknown(): Boolean =
    this?.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED) != true

internal fun isStreamingAllowed(policy: MobileDataPolicy?, isActiveNetworkMetered: Boolean): Boolean =
    !isActiveNetworkMetered || policy == MobileDataPolicy.ALLOW_STREAMING

@UnstableApi
class PolicyEnforcingDataSourceFactory(
    private val delegate: DataSource.Factory,
    private val networkPolicy: PlaybackNetworkPolicy,
    private val transferListeners: List<TransferListener> = emptyList(),
) : DataSource.Factory {
    override fun createDataSource(): DataSource = PolicyEnforcingDataSource(
        delegate.createDataSource().also { source -> transferListeners.forEach(source::addTransferListener) },
        networkPolicy::requireStreamingAllowed,
    )
}

@UnstableApi
internal class PolicyEnforcingDataSource(
    private val delegate: DataSource,
    private val requireStreamingAllowed: () -> Unit,
) : DataSource {
    override fun addTransferListener(transferListener: TransferListener) {
        delegate.addTransferListener(transferListener)
    }

    override fun open(dataSpec: DataSpec): Long {
        requireStreamingAllowed()
        return delegate.open(dataSpec)
    }

    override fun read(buffer: ByteArray, offset: Int, length: Int): Int {
        requireStreamingAllowed()
        return delegate.read(buffer, offset, length)
    }

    override fun getUri() = delegate.getUri()

    override fun getResponseHeaders(): Map<String, List<String>> = delegate.getResponseHeaders()

    override fun close() = delegate.close()
}
