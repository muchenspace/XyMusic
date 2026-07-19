package com.xymusic.app.feature.auth.domain

import com.xymusic.app.feature.auth.domain.model.DeviceInfo

fun interface DeviceInfoProvider {
    fun get(): DeviceInfo
}
