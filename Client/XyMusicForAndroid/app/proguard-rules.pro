-keepattributes Signature
-keepattributes *Annotation*

# Kotlin serialization
-keepclassmembers class **$$serializer {
    *** INSTANCE;
}
-keepclassmembers class * {
    *** Companion;
}
-keepclasseswithmembers class * {
    kotlinx.serialization.KSerializer serializer(...);
}
-keepclassmembers class * {
    @kotlinx.serialization.SerialName <fields>;
}
-keep class com.xymusic.app.core.data.media.remote.** { *; }
-keep class com.xymusic.app.core.model.media.** { *; }

# Retrofit reads HTTP annotations from service interfaces at runtime.
-keep,allowoptimization,allowshrinking interface com.xymusic.app.**.*Api
-keepattributes RuntimeVisibleAnnotations,RuntimeVisibleParameterAnnotations,AnnotationDefault

# Room loads the generated database implementation by its original class name.
-keep class com.xymusic.app.core.database.XyMusicDatabase_Impl { *; }

# Coil 3
-keep class * extends coil3.util.FetcherServiceLoaderTarget { *; }
-keep class * extends coil3.util.DecoderServiceLoaderTarget { *; }
-keep class coil3.network.okhttp.** { *; }
-keep class coil3.** { *; }
-keep interface coil3.** { *; }
-dontwarn coil3.**

# Media3 & MediaSession & Media Compat
-keep class androidx.media3.session.** { *; }
-keep interface androidx.media3.session.** { *; }
-keep class androidx.media3.common.** { *; }
-keep interface androidx.media3.common.** { *; }
-keep class android.support.v4.media.** { *; }
-keep interface android.support.v4.media.** { *; }

# OkHttp & Okio
-keepattributes EnclosingMethod,InnerClasses
-dontwarn okhttp3.**
-dontwarn okio.**
-dontwarn javax.annotation.**
-keepnames class okhttp3.internal.publicsuffix.PublicSuffixDatabase

# Palette
-keep class androidx.palette.graphics.** { *; }
