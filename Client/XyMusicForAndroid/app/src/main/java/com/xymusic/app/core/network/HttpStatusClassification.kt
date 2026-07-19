package com.xymusic.app.core.network

internal fun Int.isServiceUnavailableStatus(): Boolean = this == 502 ||
    this == 503 ||
    this == 504 ||
    this in 521..524
