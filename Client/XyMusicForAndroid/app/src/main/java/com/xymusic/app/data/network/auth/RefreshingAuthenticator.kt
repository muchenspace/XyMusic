package com.xymusic.app.data.network.auth

import com.xymusic.app.core.network.auth.RefreshOutcome
import com.xymusic.app.core.network.auth.SingleFlightRefreshCoordinator
import com.xymusic.app.core.security.AccessToken
import com.xymusic.app.data.network.AUTHORIZATION_HEADER
import com.xymusic.app.data.network.SessionRequestContext
import javax.inject.Inject
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeoutOrNull
import okhttp3.Authenticator
import okhttp3.Request
import okhttp3.Response
import okhttp3.Route

class RefreshingAuthenticator
@Inject
constructor(private val refreshCoordinator: SingleFlightRefreshCoordinator) :
    Authenticator {
    override fun authenticate(route: Route?, response: Response): Request? {
        if (response.request.tag(RefreshAttempt::class.java) != null) return null
        val requestContext =
            response.request.tag(SessionRequestContext::class.java)
                ?: return null
        val expectedIdentity = requestContext.identityOrNull() ?: return null

        val failedToken =
            response.request
                .header(AUTHORIZATION_HEADER)
                ?.takeIf { it.startsWith(BEARER_PREFIX, ignoreCase = true) }
                ?.substring(BEARER_PREFIX.length)
                ?.takeIf(String::isNotBlank)
                ?.let(AccessToken::from)
                ?: return null
        if (failedToken != requestContext.accessToken) return null

        val outcome =
            runBlocking {
                withTimeoutOrNull(MAX_REFRESH_WAIT_MS) {
                    refreshCoordinator.refresh(failedToken, expectedIdentity)
                }
            } ?: RefreshOutcome.TemporaryFailure

        return when (outcome) {
            is RefreshOutcome.Available ->
                if (
                    outcome.tokens.userId == expectedIdentity.userId &&
                    outcome.tokens.sessionId == expectedIdentity.sessionId &&
                    refreshCoordinator.isCurrentSession(expectedIdentity, outcome.tokens)
                ) {
                    response.request
                        .newBuilder()
                        .header(AUTHORIZATION_HEADER, "Bearer ${outcome.tokens.accessToken.value}")
                        .tag(RefreshAttempt::class.java, RefreshAttempt)
                        .build()
                } else {
                    null
                }
            RefreshOutcome.SessionUnavailable,
            RefreshOutcome.TemporaryFailure,
            -> null
        }
    }

    private data object RefreshAttempt

    private companion object {
        const val BEARER_PREFIX = "Bearer "
        const val MAX_REFRESH_WAIT_MS = 36_000L
    }
}
