package com.xymusic.app.feature.player.data.controller

import com.google.common.util.concurrent.ListenableFuture
import java.util.concurrent.CancellationException
import java.util.concurrent.ExecutionException
import java.util.concurrent.Executor
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException
import kotlinx.coroutines.suspendCancellableCoroutine

internal suspend fun <T> ListenableFuture<T>.awaitFuture(onCancellation: ((T) -> Unit)? = null): T =
    suspendCancellableCoroutine { continuation ->
        addListener(
            {
                try {
                    val value = get()
                    continuation.resume(value) { _, unconsumedValue, _ ->
                        onCancellation?.invoke(unconsumedValue)
                    }
                } catch (failure: CancellationException) {
                    continuation.cancel(failure)
                } catch (failure: ExecutionException) {
                    if (continuation.isActive) {
                        continuation.resumeWithException(failure.cause ?: failure)
                    }
                } catch (failure: Exception) {
                    if (continuation.isActive) continuation.resumeWithException(failure)
                }
            },
            DIRECT_FUTURE_EXECUTOR,
        )
        continuation.invokeOnCancellation { cancel(true) }
    }

private val DIRECT_FUTURE_EXECUTOR = Executor(Runnable::run)
