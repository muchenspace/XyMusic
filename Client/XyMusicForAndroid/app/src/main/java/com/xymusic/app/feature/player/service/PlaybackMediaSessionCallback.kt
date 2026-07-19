package com.xymusic.app.feature.player.service

import android.os.Bundle
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.session.MediaSession
import androidx.media3.session.SessionCommand
import androidx.media3.session.SessionCommands
import androidx.media3.session.SessionError
import androidx.media3.session.SessionResult
import com.google.common.util.concurrent.Futures
import com.google.common.util.concurrent.ListenableFuture
import com.google.common.util.concurrent.SettableFuture
import com.xymusic.app.feature.player.data.media.PlaybackMediaUri
import java.util.concurrent.Executor
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull

@UnstableApi
internal class PlaybackMediaSessionCallback(
    private val appPackageName: String,
    private val player: Player,
    private val serviceScope: CoroutineScope,
    private val initialSessionReady: CompletableDeferred<Unit>,
    private val sleepTimerController: PlaybackSleepTimerController,
) : MediaSession.Callback {
    override fun onConnect(
        session: MediaSession,
        controller: MediaSession.ControllerInfo,
    ): MediaSession.ConnectionResult = when {
        controller.packageName == appPackageName ->
            MediaSession.ConnectionResult.accept(
                MediaSession.ConnectionResult.DEFAULT_SESSION_COMMANDS
                    .buildUpon()
                    .add(PlaybackSessionCommands.SET_SLEEP_TIMER)
                    .add(PlaybackSessionCommands.GET_SLEEP_TIMER)
                    .add(PlaybackSessionCommands.CODEC_FALLBACK_APPLIED)
                    .build(),
                Player.Commands
                    .Builder()
                    .addAllCommands()
                    .build(),
            )
        controller.isTrusted ->
            MediaSession.ConnectionResult.accept(
                SessionCommands.Builder().build(),
                SYSTEM_CONTROLLER_COMMANDS,
            )
        else -> MediaSession.ConnectionResult.reject()
    }

    override fun onCustomCommand(
        session: MediaSession,
        controller: MediaSession.ControllerInfo,
        customCommand: SessionCommand,
        args: Bundle,
    ): ListenableFuture<SessionResult> {
        if (controller.packageName != appPackageName) {
            return Futures.immediateFuture(
                SessionResult(SessionError.ERROR_PERMISSION_DENIED),
            )
        }
        val result =
            when (customCommand.customAction) {
                PlaybackSessionCommands.SET_SLEEP_TIMER.customAction -> {
                    if (!args.containsKey(PlaybackSessionCommands.ARG_SLEEP_TIMER_DURATION_MS)) {
                        SessionResult(SessionError.ERROR_BAD_VALUE)
                    } else {
                        val durationMs =
                            args.getLong(
                                PlaybackSessionCommands.ARG_SLEEP_TIMER_DURATION_MS,
                            )
                        if (durationMs !in 0L..MAX_SLEEP_TIMER_DURATION_MS) {
                            SessionResult(SessionError.ERROR_BAD_VALUE)
                        } else {
                            sleepTimerController.setTimer(durationMs.takeIf { it > 0L })
                            sleepTimerController.currentResult()
                        }
                    }
                }
                PlaybackSessionCommands.GET_SLEEP_TIMER.customAction ->
                    sleepTimerController.currentResult()
                else -> SessionResult(SessionError.ERROR_NOT_SUPPORTED)
            }
        return Futures.immediateFuture(result)
    }

    override fun onAddMediaItems(
        mediaSession: MediaSession,
        controller: MediaSession.ControllerInfo,
        mediaItems: List<MediaItem>,
    ): ListenableFuture<List<MediaItem>> {
        if (controller.packageName != appPackageName) {
            return Futures.immediateFuture(emptyList())
        }
        val validItems =
            mediaItems.all { mediaItem ->
                mediaItem.mediaId.isNotBlank() &&
                    runCatching {
                        PlaybackMediaUri.trackId(requireNotNull(mediaItem.localConfiguration).uri)
                    }.isSuccess
            }
        return if (validItems) {
            Futures.immediateFuture(mediaItems)
        } else {
            Futures.immediateFailedFuture(IllegalArgumentException("Invalid playback item"))
        }
    }

    @Suppress("DEPRECATION", "OVERRIDE_DEPRECATION")
    override fun onPlaybackResumption(
        mediaSession: MediaSession,
        controller: MediaSession.ControllerInfo,
    ): ListenableFuture<MediaSession.MediaItemsWithStartPosition> = createPlaybackResumptionFuture()

    override fun onPlaybackResumption(
        mediaSession: MediaSession,
        controller: MediaSession.ControllerInfo,
        isForPlayback: Boolean,
    ): ListenableFuture<MediaSession.MediaItemsWithStartPosition> = createPlaybackResumptionFuture()

    private fun createPlaybackResumptionFuture(): ListenableFuture<MediaSession.MediaItemsWithStartPosition> {
        val future = SettableFuture.create<MediaSession.MediaItemsWithStartPosition>()
        val job =
            serviceScope.launch {
                try {
                    val ready =
                        initialSessionReady.isCompleted ||
                            withTimeoutOrNull(
                                PLAYBACK_RESUMPTION_TIMEOUT_MS,
                            ) {
                                initialSessionReady.await()
                                true
                            } == true
                    check(ready) { "Playback session restoration timed out" }
                    check(player.mediaItemCount > 0) { "No resumable playback queue" }
                    val currentIndex =
                        player.currentMediaItemIndex
                            .takeIf { it in 0 until player.mediaItemCount }
                            ?: 0
                    val mediaItems = List(player.mediaItemCount, player::getMediaItemAt)
                    future.set(
                        MediaSession.MediaItemsWithStartPosition(
                            mediaItems,
                            currentIndex,
                            player.currentPosition.coerceAtLeast(0),
                        ),
                    )
                } catch (failure: CancellationException) {
                    future.cancel(false)
                    throw failure
                } catch (failure: Exception) {
                    future.setException(failure)
                }
            }
        future.addListener(
            { if (future.isCancelled) job.cancel() },
            DIRECT_EXECUTOR,
        )
        return future
    }

    private companion object {
        const val PLAYBACK_RESUMPTION_TIMEOUT_MS = 5_000L

        val DIRECT_EXECUTOR: Executor = Executor(Runnable::run)

        val SYSTEM_CONTROLLER_COMMANDS: Player.Commands =
            Player.Commands
                .Builder()
                .addAll(
                    Player.COMMAND_PLAY_PAUSE,
                    Player.COMMAND_PREPARE,
                    Player.COMMAND_STOP,
                    Player.COMMAND_SEEK_IN_CURRENT_MEDIA_ITEM,
                    Player.COMMAND_SEEK_TO_PREVIOUS_MEDIA_ITEM,
                    Player.COMMAND_SEEK_TO_NEXT_MEDIA_ITEM,
                    Player.COMMAND_SEEK_TO_PREVIOUS,
                    Player.COMMAND_SEEK_TO_NEXT,
                    Player.COMMAND_GET_CURRENT_MEDIA_ITEM,
                    Player.COMMAND_GET_TIMELINE,
                    Player.COMMAND_GET_METADATA,
                    Player.COMMAND_GET_AUDIO_ATTRIBUTES,
                    Player.COMMAND_GET_VOLUME,
                    Player.COMMAND_GET_DEVICE_VOLUME,
                ).build()
    }
}
