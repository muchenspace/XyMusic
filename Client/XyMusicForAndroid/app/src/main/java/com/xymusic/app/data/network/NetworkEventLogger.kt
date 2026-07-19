package com.xymusic.app.data.network

import android.util.Log
import javax.inject.Inject
import javax.inject.Singleton

fun interface NetworkEventLogger {
    fun log(message: String)
}

@Singleton
class AndroidNetworkEventLogger
@Inject
constructor() : NetworkEventLogger {
    override fun log(message: String) {
        Log.d(LOG_TAG, message)
    }

    private companion object {
        const val LOG_TAG = "XyMusicNetwork"
    }
}
