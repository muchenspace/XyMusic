package com.xymusic.app.feature.catalog.data

import com.xymusic.app.core.data.media.remote.CatalogProtocolException
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.feature.catalog.data.remote.CatalogRemoteException
import com.xymusic.app.feature.catalog.domain.CatalogResult
import java.io.IOException
import java.time.Clock
import java.util.concurrent.CancellationException
import javax.inject.Inject

class CatalogRefreshExecutor
@Inject
constructor(
    private val transactionRunner: CatalogTransactionRunner,
    private val clock: Clock,
    private val serverRuntimeCoordinator: ServerRuntimeCoordinator = ServerRuntimeCoordinator(),
) {
    suspend fun <T, R> execute(request: suspend () -> T, persist: suspend (T, Long) -> R): CatalogResult<R> = try {
        val generation = serverRuntimeCoordinator.captureGeneration()
        val detail = request()
        val result =
            transactionRunner.run {
                serverRuntimeCoordinator.requireCurrent(generation)
                persist(detail, clock.millis())
            }
        CatalogResult.Success(result)
    } catch (failure: CancellationException) {
        throw failure
    } catch (failure: CatalogRemoteException) {
        CatalogResult.Failure(failure.domainError)
    } catch (_: CatalogProtocolException) {
        protocolFailure()
    } catch (_: IOException) {
        CatalogResult.Failure(DomainError.Network("Unable to reach the catalog service"))
    } catch (_: Exception) {
        protocolFailure()
    }

    private fun protocolFailure(): CatalogResult.Failure = CatalogResult.Failure(
        DomainError.Protocol(
            detail = "Invalid catalog response",
            traceId = null,
            status = null,
        ),
    )
}
