package com.xymusic.app.core.network

import javax.inject.Qualifier

@Qualifier
@Retention(AnnotationRetention.BINARY)
annotation class AuthHttpClient

@Qualifier
@Retention(AnnotationRetention.BINARY)
annotation class ApiHttpClient

@Qualifier
@Retention(AnnotationRetention.BINARY)
annotation class ApiCallFactory

@Qualifier
@Retention(AnnotationRetention.BINARY)
annotation class MediaHttpClient

@Qualifier
@Retention(AnnotationRetention.BINARY)
annotation class ApiBaseUrl
