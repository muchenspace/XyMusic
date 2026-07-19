package com.xymusic.app.feature.player.service

import android.app.PendingIntent
import android.content.Intent
import android.os.Bundle
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import androidx.media3.session.DefaultMediaNotificationProvider
import androidx.media3.session.MediaSession
import androidx.media3.session.MediaSessionService
import com.xymusic.app.MainActivity
import com.xymusic.app.R
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.feature.player.data.media.PlaybackDataSourceFactory
import com.xymusic.app.feature.player.domain.PlaybackEventSink
import com.xymusic.app.feature.player.domain.PlaybackGrantRepository
import com.xymusic.app.feature.player.domain.PlaybackQueueStore
import dagger.hilt.android.AndroidEntryPoint
import java.time.Clock
import javax.inject.Inject
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.launch

@AndroidEntryPoint
@UnstableApi
class PlaybackService : MediaSessionService() {
    @Inject
    @PlaybackDataSourceFactory
    lateinit var playbackDataSourceFactory: DataSource.Factory

    @Inject
    lateinit var queueStore: PlaybackQueueStore

    @Inject
    lateinit var grantRepository: PlaybackGrantRepository

    @Inject
    lateinit var eventSink: PlaybackEventSink

    @Inject
    lateinit var sessionProvider: AppSessionProvider

    @Inject
    lateinit var clock: Clock

    private val serviceScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val initialSessionReady = CompletableDeferred<Unit>()

    private lateinit var player: ExoPlayer
    private lateinit var mediaSession: MediaSession
    private lateinit var persistenceController: PlaybackPersistenceController
    private lateinit var sleepTimerController: PlaybackSleepTimerController
    private lateinit var codecFallbackController: PlaybackCodecFallbackController

    override fun onCreate() {
        super.onCreate()

        player = createPlayer()
        sleepTimerController =
            PlaybackSleepTimerController(
                serviceScope = serviceScope,
                player = player,
                mediaSession = { mediaSession },
            )
        persistenceController =
            PlaybackPersistenceController(
                player = player,
                serviceScope = serviceScope,
                queueStore = queueStore,
                eventSink = eventSink,
                clock = clock,
                cancelSleepTimer = sleepTimerController::cancelForAccountChange,
                clearPlaybackGrants = grantRepository::clear,
            )
        mediaSession =
            createMediaSession(
                PlaybackMediaSessionCallback(
                    appPackageName = packageName,
                    player = player,
                    serviceScope = serviceScope,
                    initialSessionReady = initialSessionReady,
                    sleepTimerController = sleepTimerController,
                ),
            )
        codecFallbackController =
            PlaybackCodecFallbackController(
                player = player,
                grantRepository = grantRepository,
                onFallbackApplied = {
                    mediaSession.broadcastCustomCommand(
                        PlaybackSessionCommands.CODEC_FALLBACK_APPLIED,
                        Bundle.EMPTY,
                    )
                },
            )
        player.addListener(codecFallbackController)

        setMediaNotificationProvider(
            DefaultMediaNotificationProvider(this).apply {
                setSmallIcon(R.drawable.ic_stat_xymusic)
            },
        )

        serviceScope.launch {
            sessionProvider.restoreSession()
            sessionProvider.sessionState.collectLatest { state ->
                handleSessionState(state)
                if (state != AppSessionState.Loading) initialSessionReady.complete(Unit)
            }
        }
    }

    override fun onGetSession(controllerInfo: MediaSession.ControllerInfo): MediaSession = mediaSession

    override fun onTaskRemoved(rootIntent: Intent?) {
        val stopAfterFlush = !player.playWhenReady && !isPlaybackOngoing
        persistenceController.flushForTaskRemoval(stopAfterFlush) {
            stopSelf()
        }
        // MediaSessionService would stop an idle service immediately, before this flush completes.
    }

    override fun onDestroy() {
        if (::persistenceController.isInitialized) {
            persistenceController.cancelForDestroy {
                if (::sleepTimerController.isInitialized) {
                    sleepTimerController.cancelPendingJob()
                }
            }
        } else if (::sleepTimerController.isInitialized) {
            sleepTimerController.cancelPendingJob()
        }
        serviceScope.cancel()
        if (::codecFallbackController.isInitialized && ::player.isInitialized) {
            player.removeListener(codecFallbackController)
        }
        if (::mediaSession.isInitialized) mediaSession.release()
        if (::player.isInitialized) player.release()
        super.onDestroy()
    }

    private fun createPlayer(): ExoPlayer {
        val audioAttributes =
            AudioAttributes
                .Builder()
                .setContentType(C.AUDIO_CONTENT_TYPE_MUSIC)
                .setUsage(C.USAGE_MEDIA)
                .build()
        return ExoPlayer
            .Builder(
                this,
                DefaultMediaSourceFactory(this).setDataSourceFactory(playbackDataSourceFactory),
            ).setAudioAttributes(audioAttributes, true)
            .setHandleAudioBecomingNoisy(true)
            .setWakeMode(C.WAKE_MODE_NETWORK)
            .build()
    }

    private fun createMediaSession(callback: MediaSession.Callback): MediaSession {
        val sessionActivity =
            PendingIntent.getActivity(
                this,
                0,
                Intent(this, MainActivity::class.java),
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
            )
        return MediaSession
            .Builder(this, player)
            .setSessionActivity(sessionActivity)
            .setCallback(callback)
            .setBitmapLoader(
                PlaybackArtworkBitmapLoader(
                    context = applicationContext,
                    scope = serviceScope,
                ),
            )
            .build()
    }

    private suspend fun handleSessionState(state: AppSessionState) {
        when (state) {
            AppSessionState.Loading -> Unit
            AppSessionState.SignedOut -> {
                codecFallbackController.resetForAccountChange()
                persistenceController.clearForAccountChange(null)
            }
            is AppSessionState.SignedIn -> {
                if (persistenceController.isActiveUser(state.userId)) return
                codecFallbackController.resetForAccountChange()
                persistenceController.clearForAccountChange(state.userId)
                persistenceController.restoreQueue()
            }
        }
    }
}
