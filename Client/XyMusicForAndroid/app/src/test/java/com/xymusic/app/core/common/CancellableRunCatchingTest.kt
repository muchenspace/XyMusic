package com.xymusic.app.core.common

import com.google.common.truth.Truth.assertThat
import kotlinx.coroutines.CancellationException
import org.junit.Assert.assertThrows
import org.junit.Test

class CancellableRunCatchingTest {
    @Test
    fun cancellationIsRethrownUnchanged() {
        val cancellation = CancellationException("cancelled")

        val thrown =
            assertThrows(CancellationException::class.java) {
                runCatchingPreservingCancellation { throw cancellation }
            }

        assertThat(thrown).isSameInstanceAs(cancellation)
    }

    @Test
    fun regularFailureIsCaptured() {
        val failure = IllegalStateException("failed")

        val result = runCatchingPreservingCancellation<Unit> { throw failure }

        assertThat(result.exceptionOrNull()).isSameInstanceAs(failure)
    }
}
