package com.xymusic.app.feature.library.data

import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.network.ServerGeneration
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.network.model.ProblemCode
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.feature.library.data.remote.LibraryRemoteException
import com.xymusic.app.feature.library.domain.LibraryResult
import java.io.IOException
import java.util.concurrent.CancellationException
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.withContext

internal class LibraryRepositoryExecutionContext(
    private val sessionProvider: AppSessionProvider,
    private val sessionMutationCoordinator: SessionMutationCoordinator,
    private val serverRuntimeCoordinator: ServerRuntimeCoordinator,
    private val ioDispatcher: CoroutineDispatcher,
) {
    fun captureServerGeneration(): ServerGeneration = serverRuntimeCoordinator.captureGeneration()

    fun requireOwner(): String = (sessionProvider.sessionState.value as? AppSessionState.SignedIn)?.userId
        ?: throw SignedOutException

    suspend fun <T> withActiveOwner(
        owner: String,
        serverGeneration: ServerGeneration? = null,
        block: suspend () -> T,
    ): T = sessionMutationCoordinator.mutate {
        if ((sessionProvider.sessionState.value as? AppSessionState.SignedIn)?.userId != owner) {
            throw SignedOutException
        }
        serverGeneration?.let(serverRuntimeCoordinator::requireCurrent)
        block()
    }

    suspend fun <T> ioCall(block: suspend () -> LibraryResult<T>): LibraryResult<T> = withContext(ioDispatcher) {
        try {
            block()
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: SignedOutException) {
            LibraryResult.Failure(authenticationFailure())
        } catch (failure: LibraryRemoteException) {
            LibraryResult.Failure(failure.error)
        } catch (_: IOException) {
            LibraryResult.Failure(DomainError.Network("Unable to reach the server"))
        } catch (failure: LocalLibraryException) {
            localFailure(requireNotNull(failure.message))
        } catch (_: Exception) {
            localFailure("Unable to update the music library")
        }
    }

    private fun authenticationFailure() = DomainError.Authentication(
        detail = "Authentication is required",
        traceId = null,
        reason = ProblemCode.AuthenticationRequired,
    )

    private fun localFailure(detail: String) = LibraryResult.Failure(
        DomainError.Protocol(detail, null, null),
    )
}

internal object SignedOutException : IllegalStateException()

internal class LocalLibraryException(message: String) : IllegalStateException(message)
