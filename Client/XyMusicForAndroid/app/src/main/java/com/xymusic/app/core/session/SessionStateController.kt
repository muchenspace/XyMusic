package com.xymusic.app.core.session

import com.xymusic.app.core.security.SessionTokens

interface SessionStateController {
    fun onSessionAvailable(tokens: SessionTokens)

    fun onSessionCleared()
}
