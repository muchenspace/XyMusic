package com.xymusic.app.core.network

import com.xymusic.app.domain.server.ServerEndpoint
import okhttp3.HttpUrl

val ServerEndpoint.baseUrl: HttpUrl
    get() =
        HttpUrl
            .Builder()
            .scheme(protocol.scheme)
            .host(host)
            .port(port)
            .addPathSegment("")
            .build()
