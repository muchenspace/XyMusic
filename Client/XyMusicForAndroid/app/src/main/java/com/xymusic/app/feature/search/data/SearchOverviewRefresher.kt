package com.xymusic.app.feature.search.data

import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.feature.search.data.remote.SearchProtocolException
import com.xymusic.app.feature.search.data.remote.SearchRemoteDataSource
import com.xymusic.app.feature.search.data.remote.SearchRemoteException
import com.xymusic.app.feature.search.domain.SearchResult
import com.xymusic.app.feature.search.domain.model.SearchQuery
import java.io.IOException
import java.util.concurrent.CancellationException
import javax.inject.Inject

class SearchOverviewRefresher
@Inject
constructor(
    private val remote: SearchRemoteDataSource,
    private val store: SearchOverviewStore,
    private val serverRuntimeCoordinator: ServerRuntimeCoordinator = ServerRuntimeCoordinator(),
) {
    suspend fun refresh(query: SearchQuery): SearchResult<Unit> = try {
        val generation = serverRuntimeCoordinator.captureGeneration()
        val overview = remote.overview(query)
        serverRuntimeCoordinator.requireCurrent(generation)
        store.replaceIfCurrent(query, overview, generation)
        SearchResult.Success(Unit)
    } catch (failure: CancellationException) {
        throw failure
    } catch (failure: SearchRemoteException) {
        SearchResult.Failure(failure.domainError)
    } catch (_: SearchProtocolException) {
        protocolFailure()
    } catch (_: IOException) {
        SearchResult.Failure(DomainError.Network("Unable to reach the search service"))
    } catch (_: Exception) {
        protocolFailure()
    }

    private fun protocolFailure(): SearchResult.Failure = SearchResult.Failure(
        DomainError.Protocol("Invalid search response", null, null),
    )
}
