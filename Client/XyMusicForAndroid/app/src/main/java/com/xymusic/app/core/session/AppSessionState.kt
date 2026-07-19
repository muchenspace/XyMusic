package com.xymusic.app.core.session

sealed interface AppSessionState {
    data object Loading : AppSessionState

    data object SignedOut : AppSessionState

    data class SignedIn(val userId: String) : AppSessionState
}
