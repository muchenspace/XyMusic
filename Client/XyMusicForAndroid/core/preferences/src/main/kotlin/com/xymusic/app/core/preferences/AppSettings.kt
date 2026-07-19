package com.xymusic.app.core.preferences

data class AppSettings(
    val theme: ThemePreference = ThemePreference.SYSTEM,
    val dynamicColorEnabled: Boolean = false,
    val wordByWordLyricsEnabled: Boolean = true,
    val streamingQuality: StreamingQuality = StreamingQuality.AUTO,
    val mobileDataPolicy: MobileDataPolicy = MobileDataPolicy.ALLOW_STREAMING,
    val cacheLimitMiB: Int = 512,
) {
    init {
        require(cacheLimitMiB in 128..4_096) { "cacheLimitMiB must be between 128 and 4096" }
    }
}

enum class ThemePreference(val supportsDynamicColor: Boolean) {
    SYSTEM(true),
    LIGHT(true),
    DARK(true),
    PEACH_PINK(false),
    OCEAN_BLUE(false),
    TWILIGHT_PURPLE(false),
}

enum class StreamingQuality { AUTO, DATA_SAVER, STANDARD, HIGH, LOSSLESS }

enum class MobileDataPolicy { ALLOW_STREAMING, WIFI_ONLY }
