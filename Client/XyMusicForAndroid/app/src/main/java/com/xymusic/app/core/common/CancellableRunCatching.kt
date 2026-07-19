package com.xymusic.app.core.common

import kotlinx.coroutines.CancellationException

internal inline fun <T> runCatchingPreservingCancellation(block: () -> T): Result<T> =
    runCatching(block).onFailure { failure ->
        if (failure is CancellationException) throw failure
    }
