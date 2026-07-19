package com.xymusic.app.data.network.auth

import com.xymusic.app.core.network.AuthHttpClient
import java.io.IOException
import javax.inject.Inject
import kotlin.coroutines.resumeWithException
import kotlinx.coroutines.suspendCancellableCoroutine
import okhttp3.Call
import okhttp3.Callback
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response

fun interface AuthCallExecutor {
    @Throws(IOException::class)
    suspend fun execute(request: Request): Response
}

class OkHttpAuthCallExecutor
@Inject
constructor(@AuthHttpClient private val client: OkHttpClient) :
    AuthCallExecutor {
    override suspend fun execute(request: Request): Response = suspendCancellableCoroutine { continuation ->
        val call = client.newCall(request)
        continuation.invokeOnCancellation { call.cancel() }
        call.enqueue(
            object : Callback {
                override fun onFailure(call: Call, e: IOException) {
                    if (continuation.isActive) continuation.resumeWithException(e)
                }

                override fun onResponse(call: Call, response: Response) {
                    if (!continuation.isActive) {
                        response.close()
                        return
                    }
                    continuation.resume(response) { _, unconsumedResponse, _ ->
                        unconsumedResponse.close()
                    }
                }
            },
        )
    }
}
