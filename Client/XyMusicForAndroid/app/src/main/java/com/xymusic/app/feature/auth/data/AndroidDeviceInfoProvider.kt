package com.xymusic.app.feature.auth.data

import android.annotation.SuppressLint
import android.content.Context
import android.os.Build
import com.xymusic.app.BuildConfig
import com.xymusic.app.feature.auth.domain.DeviceInfoProvider
import com.xymusic.app.feature.auth.domain.model.DeviceInfo
import dagger.hilt.android.qualifiers.ApplicationContext
import java.util.UUID
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
@SuppressLint("UseKtx") // The installation ID must be durable before a login request is sent.
class AndroidDeviceInfoProvider
@Inject
constructor(@ApplicationContext private val context: Context) :
    DeviceInfoProvider {
    private val preferences by lazy(LazyThreadSafetyMode.SYNCHRONIZED) {
        context.getSharedPreferences(PREFERENCES_NAME, Context.MODE_PRIVATE)
    }

    override fun get(): DeviceInfo = DeviceInfo(
        installationId = installationId(),
        name = deviceName(),
        appVersion = BuildConfig.VERSION_NAME,
    )

    @Synchronized
    private fun installationId(): String {
        val stored =
            preferences
                .getString(KEY_INSTALLATION_ID, null)
                ?.takeIf(::isUuid)
        if (stored != null) return stored

        val generated = UUID.randomUUID().toString()
        check(preferences.edit().putString(KEY_INSTALLATION_ID, generated).commit()) {
            "Unable to persist installation ID"
        }
        return generated
    }

    private fun deviceName(): String {
        val manufacturer = Build.MANUFACTURER.orEmpty().trim()
        val model = Build.MODEL.orEmpty().trim()
        return listOf(manufacturer, model)
            .filter(String::isNotBlank)
            .joinToString(" ")
            .ifBlank { DEFAULT_DEVICE_NAME }
            .take(MAX_DEVICE_NAME_LENGTH)
    }

    private fun isUuid(value: String): Boolean = runCatching {
        UUID.fromString(value)
    }.isSuccess

    private companion object {
        const val PREFERENCES_NAME = "auth_device_info"
        const val KEY_INSTALLATION_ID = "installation_id"
        const val DEFAULT_DEVICE_NAME = "Android device"
        const val MAX_DEVICE_NAME_LENGTH = 100
    }
}
