package com.xymusic.app.feature.catalog.data

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.feature.catalog.domain.CatalogResult
import java.io.IOException
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.test.runTest
import org.junit.Test

class CatalogRefreshExecutorTest {
    private val clock = Clock.fixed(Instant.parse("2026-07-11T00:00:00Z"), ZoneOffset.UTC)

    @Test
    fun offlineRefreshDoesNotEnterTransactionOrMutateCachedDetail() = runTest {
        var transactionCalls = 0
        var persistCalls = 0
        val executor =
            CatalogRefreshExecutor(
                transactionRunner =
                object : CatalogTransactionRunner {
                    override suspend fun <T> run(block: suspend () -> T): T {
                        transactionCalls += 1
                        return block()
                    }
                },
                clock = clock,
            )

        val result =
            executor.execute(
                request = { throw IOException("offline") },
                persist = { _: String, _ -> persistCalls += 1 },
            )

        assertThat(result).isInstanceOf(CatalogResult.Failure::class.java)
        assertThat((result as CatalogResult.Failure).error)
            .isInstanceOf(DomainError.Network::class.java)
        assertThat(transactionCalls).isEqualTo(0)
        assertThat(persistCalls).isEqualTo(0)
    }

    @Test
    fun successfulRefreshPersistsInsideOneTransactionWithSharedTimestamp() = runTest {
        var transactionCalls = 0
        var persistedAt: Long? = null
        val executor =
            CatalogRefreshExecutor(
                transactionRunner =
                object : CatalogTransactionRunner {
                    override suspend fun <T> run(block: suspend () -> T): T {
                        transactionCalls += 1
                        return block()
                    }
                },
                clock = clock,
            )

        val result =
            executor.execute(
                request = { "detail" },
                persist = { _, cachedAt -> persistedAt = cachedAt },
            )

        assertThat(result).isEqualTo(CatalogResult.Success(Unit))
        assertThat(transactionCalls).isEqualTo(1)
        assertThat(persistedAt).isEqualTo(clock.millis())
    }

    @Test
    fun successfulRefreshReturnsValueCreatedInsideTransaction() = runTest {
        val executor =
            CatalogRefreshExecutor(
                transactionRunner =
                object : CatalogTransactionRunner {
                    override suspend fun <T> run(block: suspend () -> T): T = block()
                },
                clock = clock,
            )

        val result =
            executor.execute(
                request = { "response" },
                persist = { response, cachedAt -> "$response@$cachedAt" },
            )

        assertThat(result).isEqualTo(CatalogResult.Success("response@${clock.millis()}"))
    }

    @Test
    fun responseFromPreviousServerGenerationIsNotPersisted() = runTest {
        val runtime = ServerRuntimeCoordinator()
        val requestStarted = CompletableDeferred<Unit>()
        val releaseResponse = CompletableDeferred<Unit>()
        var persistCalls = 0
        val executor =
            CatalogRefreshExecutor(
                transactionRunner =
                object : CatalogTransactionRunner {
                    override suspend fun <T> run(block: suspend () -> T): T = block()
                },
                clock = clock,
                serverRuntimeCoordinator = runtime,
            )
        val refresh =
            async {
                executor.execute(
                    request = {
                        requestStarted.complete(Unit)
                        releaseResponse.await()
                        "stale response"
                    },
                    persist = { _, _ -> persistCalls += 1 },
                )
            }
        requestStarted.await()

        val switchGeneration = runtime.beginSwitch()
        releaseResponse.complete(Unit)
        val result = refresh.await()
        runtime.finishSwitch(switchGeneration)

        assertThat(result).isInstanceOf(CatalogResult.Failure::class.java)
        assertThat((result as CatalogResult.Failure).error)
            .isInstanceOf(DomainError.Network::class.java)
        assertThat(persistCalls).isEqualTo(0)
    }
}
