package com.xymusic.app.feature.auth.data.remote

import com.xymusic.app.data.network.SessionRequestContext
import com.xymusic.app.data.network.auth.model.AuthSessionDto
import retrofit2.Response
import retrofit2.http.Body
import retrofit2.http.POST
import retrofit2.http.Tag

interface PublicAuthApi {
    @POST("api/v1/auth/register")
    suspend fun register(@Body request: RegisterRequestDto): Response<RegistrationResultDto>

    @POST("api/v1/auth/login")
    suspend fun login(@Body request: LoginRequestDto): Response<AuthSessionDto>
}

fun interface SessionAuthApi {
    @POST("api/v1/auth/logout")
    suspend fun logout(@Tag requestContext: SessionRequestContext): Response<Unit>
}
