package com.xymusic.app.data.network

import android.content.Context
import com.xymusic.app.core.common.IoDispatcher
import com.xymusic.app.core.network.ServerConfigRepository
import com.xymusic.app.core.network.ServerEndpoint
import com.xymusic.app.core.network.ServerProtocol
import dagger.hilt.android.qualifiers.ApplicationContext
import java.io.IOException
import java.util.concurrent.CancellationException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext

@Singleton
class SharedPreferencesServerConfigRepository
@Inject
internal constructor(
    @ApplicationContext private val context: Context,
    @IoDispatcher private val ioDispatcher: CoroutineDispatcher = Dispatchers.IO,
) : ServerConfigRepository {
    private val preferences by lazy(LazyThreadSafetyMode.SYNCHRONIZED) {
        context.getSharedPreferences(PREFERENCES_NAME, Context.MODE_PRIVATE)
    }
    private val mutableEndpoint = MutableStateFlow<ServerEndpoint?>(null)
    private val stateMutex = Mutex()

    @Volatile
    private var loaded = false

    override val endpoint: StateFlow<ServerEndpoint?> = mutableEndpoint.asStateFlow()

    override suspend fun load() {
        if (loaded) return
        withContext(ioDispatcher) {
            stateMutex.withLock {
                if (loaded) return@withLock
                mutableEndpoint.value =
                    try {
                        readEndpoint()
                    } catch (failure: CancellationException) {
                        throw failure
                    } catch (_: Exception) {
                        null
                    }
                loaded = true
            }
        }
    }

    override fun currentEndpoint(): ServerEndpoint? = mutableEndpoint.value

    override suspend fun update(endpoint: ServerEndpoint) {
        load()
        withContext(ioDispatcher) {
            stateMutex.withLock {
                val saved =
                    preferences
                        .edit()
                        .putString(KEY_PROTOCOL, endpoint.protocol.name)
                        .putString(KEY_HOST, endpoint.host)
                        .putInt(KEY_PORT, endpoint.port)
                        .commit()
                if (!saved) throw IOException("Unable to persist server configuration")
                mutableEndpoint.value = endpoint
            }
        }
    }

    private fun readEndpoint(): ServerEndpoint? {
        val host = preferences.getString(KEY_HOST, null) ?: return null
        val port = preferences.getInt(KEY_PORT, -1).takeIf { it > 0 } ?: return null
        val protocol =
            preferences
                .getString(KEY_PROTOCOL, null)
                ?.let { value -> ServerProtocol.entries.firstOrNull { it.name == value } }
                ?: ServerProtocol.HTTPS
        return ServerEndpoint.parse(host, port.toString(), protocol)
    }

    private companion object {
        const val PREFERENCES_NAME = "xy_music_server_config"
        const val KEY_PROTOCOL = "protocol"
        const val KEY_HOST = "host"
        const val KEY_PORT = "port"
    }
}
