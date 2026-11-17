-keepattributes Signature
-keepattributes *Annotation*

# Kotlin serialization
-keep,includedescriptorclasses class **$$serializer { *; }
-if @kotlinx.serialization.Serializable class **
-keepclassmembers class <1> {
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

# Coil 3 ServiceLoader targets
-keep class * extends coil3.util.FetcherServiceLoaderTarget { *; }
-keep class * extends coil3.util.DecoderServiceLoaderTarget { *; }
-dontwarn coil3.**

# Media Compat Parcelables
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
