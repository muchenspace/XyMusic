package com.xymusic.app.data.network.auth

import com.google.common.truth.Truth.assertThat
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.test.runTest
import okhttp3.Call
import okhttp3.EventListener
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.Test

class AuthCallExecutorTest {
    @Test
    fun cancellingTheCoroutineCancelsTheUnderlyingCall() = runTest {
        val allowResponse = CountDownLatch(1)
        val server = MockWebServer()
        server.dispatcher =
            object : Dispatcher() {
                override fun dispatch(request: RecordedRequest): MockResponse {
                    allowResponse.await()
                    return MockResponse().setResponseCode(200)
                }
            }
        server.start()
        try {
            val observedCall = AtomicReference<Call>()
            val client =
                OkHttpClient
                    .Builder()
                    .eventListener(
                        object : EventListener() {
                            override fun callStart(call: Call) {
                                observedCall.set(call)
                            }
                        },
                    ).build()
            val executor = OkHttpAuthCallExecutor(client)
            val execution =
                backgroundScope.async(Dispatchers.IO) {
                    executor.execute(Request.Builder().url(server.url("/refresh")).build()).close()
                }
            assertThat(server.takeRequest(5, TimeUnit.SECONDS)).isNotNull()

            execution.cancelAndJoin()

            assertThat(observedCall.get().isCanceled()).isTrue()
        } finally {
            allowResponse.countDown()
            server.shutdown()
        }
    }
}
