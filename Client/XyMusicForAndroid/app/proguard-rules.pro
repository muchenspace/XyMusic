-keepattributes Signature
-keepattributes *Annotation*

# Kotlin serialization generates serializers at compile time.
-keepclassmembers class **$$serializer {
    *** INSTANCE;
}

# Retrofit reads HTTP annotations from service interfaces at runtime.
-keep,allowoptimization,allowshrinking interface com.xymusic.app.**.*Api
-keepattributes RuntimeVisibleAnnotations,RuntimeVisibleParameterAnnotations,AnnotationDefault

# Room loads the generated database implementation by its original class name.
-keep class com.xymusic.app.core.database.XyMusicDatabase_Impl { *; }
