package com.xymusic.app.feature.player.data.controller

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.os.Bundle
import androidx.media3.session.MediaController
import androidx.media3.session.SessionCommand
import androidx.media3.session.SessionResult
import androidx.media3.session.SessionToken
import com.google.common.util.concurrent.Futures
import com.google.common.util.concurrent.ListenableFuture
import com.xymusic.app.feature.player.adapter.media3.PlaybackServiceContract
import dagger.hilt.android.qualifiers.ApplicationContext
import java.util.concurrent.CopyOnWriteArraySet
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

@Singleton
class MediaControllerConnection
@Inject
constructor(@ApplicationContext private val context: Context) {
    private val mutex = Mutex()
    private val controllerLock = Any()
    private val listeners = CopyOnWriteArraySet<Listener>()
    private var controller: MediaController? = null
    private val controllerListener =
        object : MediaController.Listener {
            override fun onDisconnected(controller: MediaController) {
                handleDisconnected(controller)
            }

            override fun onCustomCommand(
                controller: MediaController,
                command: SessionCommand,
                args: Bundle,
            ): ListenableFuture<SessionResult> {
                val handled =
                    listeners.any { listener ->
                        runCatching { listener.onCustomCommand(controller, command, args) }
                            .getOrDefault(false)
                    }
                return Futures.immediateFuture(
                    SessionResult(
                        if (handled) {
                            SessionResult.RESULT_SUCCESS
                        } else {
                            SessionResult.RESULT_ERROR_NOT_SUPPORTED
                        },
                    ),
                )
            }
        }

    suspend fun connect(): MediaController = mutex.withLock {
        synchronized(controllerLock) { controller }
            ?.takeIf { it.isConnected }
            ?.let { return@withLock it }
        takeController()?.release()
        val serviceIntent =
            Intent().setComponent(
                ComponentName(context, PlaybackServiceContract.SERVICE_CLASS_NAME),
            )
        runCatching {
            context.startService(serviceIntent)
        }
        val token = SessionToken(
            context,
            ComponentName(context, PlaybackServiceContract.SERVICE_CLASS_NAME),
        )
        MediaController
            .Builder(context, token)
            .setListener(controllerListener)
            .buildAsync()
            .awaitFuture(MediaController::release)
            .also { connectedController ->
                synchronized(controllerLock) { controller = connectedController }
            }
    }

    fun current(): MediaController? {
        val currentController = synchronized(controllerLock) { controller } ?: return null
        if (currentController.isConnected) return currentController
        handleDisconnected(currentController)
        return null
    }

    fun addListener(listener: Listener) {
        listeners += listener
    }

    fun removeListener(listener: Listener) {
        listeners -= listener
    }

    suspend fun disconnect() = mutex.withLock {
        takeController()?.release()
    }

    private fun handleDisconnected(disconnectedController: MediaController) {
        val wasCurrent =
            synchronized(controllerLock) {
                if (controller !== disconnectedController) {
                    false
                } else {
                    controller = null
                    true
                }
            }
        if (!wasCurrent) return
        disconnectedController.release()
        listeners.forEach { it.onDisconnected(disconnectedController) }
    }

    private fun takeController(): MediaController? = synchronized(controllerLock) {
        controller.also { controller = null }
    }

    interface Listener {
        fun onDisconnected(controller: MediaController)

        fun onCustomCommand(controller: MediaController, command: SessionCommand, args: Bundle): Boolean = false
    }
}
