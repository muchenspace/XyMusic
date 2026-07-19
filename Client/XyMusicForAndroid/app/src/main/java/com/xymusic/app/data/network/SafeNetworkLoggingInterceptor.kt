package com.xymusic.app.data.network

import java.util.concurrent.TimeUnit
import okhttp3.Interceptor
import okhttp3.Response

class SafeNetworkLoggingInterceptor(private val logger: NetworkEventLogger, private val enabled: Boolean) :
    Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        if (!enabled) return chain.proceed(chain.request())

        val request = chain.request()
        val safeTarget = "${request.url.scheme}://${request.url.host}${request.url.encodedPath}"
        val startedAt = System.nanoTime()
        logger.log("--> ${request.method} $safeTarget")
        return try {
            chain.proceed(request).also { response ->
                val durationMs = TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - startedAt)
                val traceId = response.header(TRACE_ID_HEADER)?.take(MAX_TRACE_ID_LOG_LENGTH)
                val traceSuffix = traceId?.let { " traceId=$it" }.orEmpty()
                logger.log("<-- ${response.code} ${request.method} $safeTarget ${durationMs}ms$traceSuffix")
            }
        } catch (error: Exception) {
            val durationMs = TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - startedAt)
            logger.log("<-- FAILED ${request.method} $safeTarget ${durationMs}ms ${error::class.simpleName}")
            throw error
        }
    }

    private companion object {
        const val TRACE_ID_HEADER = "X-Trace-Id"
        const val MAX_TRACE_ID_LOG_LENGTH = 128
    }
}
