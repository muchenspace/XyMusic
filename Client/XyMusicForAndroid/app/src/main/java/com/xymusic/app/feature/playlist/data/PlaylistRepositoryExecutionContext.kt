package com.xymusic.app.feature.playlist.data

import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.network.ServerGeneration
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.network.model.ProblemCode
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.feature.playlist.data.remote.PlaylistRemoteException
import com.xymusic.app.feature.playlist.domain.PlaylistResult
import java.io.IOException
import java.util.concurrent.CancellationException
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.withContext

internal class PlaylistRepositoryExecutionContext(
    private val sessionProvider: AppSessionProvider,
    private val sessionMutationCoordinator: SessionMutationCoordinator,
    private val serverRuntimeCoordinator: ServerRuntimeCoordinator,
    private val ioDispatcher: CoroutineDispatcher,
) {
    private val mutationCoordinator = PlaylistMutationCoordinator()

    fun requireOwner(): String = (sessionProvider.sessionState.value as? AppSessionState.SignedIn)?.userId
        ?: throw PlaylistSignedOutException

    fun captureServerGeneration(): ServerGeneration = serverRuntimeCoordinator.captureGeneration()

    fun requireCurrent(serverGeneration: ServerGeneration) {
        serverRuntimeCoordinator.requireCurrent(serverGeneration)
    }

    suspend fun <T> withActiveOwner(
        owner: String,
        serverGeneration: ServerGeneration? = null,
        block: suspend () -> T,
    ): T = sessionMutationCoordinator.mutate {
        if ((sessionProvider.sessionState.value as? AppSessionState.SignedIn)?.userId != owner) {
            throw PlaylistSignedOutException
        }
        serverGeneration?.let(serverRuntimeCoordinator::requireCurrent)
        block()
    }

    suspend fun <T> serializePlaylistMutation(
        playlistId: String,
        block: suspend () -> PlaylistResult<T>,
    ): PlaylistResult<T> = mutationCoordinator.serialize(playlistId) {
        ioCall(block)
    }

    suspend fun <T> ioCall(block: suspend () -> PlaylistResult<T>): PlaylistResult<T> = withContext(ioDispatcher) {
        try {
            block()
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: PlaylistSignedOutException) {
            PlaylistResult.Failure(authenticationFailure())
        } catch (failure: PlaylistRemoteException) {
            failure.conflict?.let { conflict -> PlaylistResult.Conflict(conflict) }
                ?: PlaylistResult.Failure(failure.error)
        } catch (_: IOException) {
            PlaylistResult.Failure(DomainError.Network("Unable to reach the server"))
        } catch (_: Exception) {
            protocolFailure("Unable to update the playlist")
        }
    }
}

internal fun authenticationFailure() = DomainError.Authentication(
    "Authentication is required",
    null,
    ProblemCode.AuthenticationRequired,
)

internal fun protocolFailure(detail: String) = PlaylistResult.Failure(
    DomainError.Protocol(detail, null, null),
)

private object PlaylistSignedOutException : IllegalStateException()
