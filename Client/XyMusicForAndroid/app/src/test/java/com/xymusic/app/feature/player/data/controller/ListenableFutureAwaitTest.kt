package com.xymusic.app.feature.player.data.controller

import com.google.common.truth.Truth.assertThat
import com.google.common.util.concurrent.SettableFuture
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class ListenableFutureAwaitTest {
    @Test
    fun completedValueIsReturned() = runTest {
        val future = SettableFuture.create<String>()
        future.set("ready")

        assertThat(future.awaitFuture()).isEqualTo("ready")
    }

    @Test
    fun cancellationCancelsPendingFuture() = runTest {
        val future = SettableFuture.create<String>()
        val result = async { future.awaitFuture() }
        runCurrent()

        result.cancel()
        runCurrent()

        assertThat(future.isCancelled).isTrue()
    }

    @Test
    fun cancellationCleansUpCompletedButUnconsumedValue() = runTest {
        val future = SettableFuture.create<String>()
        var cleanedValue: String? = null
        val result = async { future.awaitFuture { cleanedValue = it } }
        runCurrent()

        future.set("controller")
        result.cancel()
        runCurrent()

        assertThat(cleanedValue).isEqualTo("controller")
    }

    @Test
    fun executionFailureIsUnwrapped() = runTest {
        val future = SettableFuture.create<String>()
        val expected = IllegalStateException("connection failed")
        future.setException(expected)

        val failure = runCatching { future.awaitFuture() }.exceptionOrNull()
        assertThat(failure).isInstanceOf(IllegalStateException::class.java)
        assertThat(failure).hasMessageThat().isEqualTo(expected.message)
    }
}
