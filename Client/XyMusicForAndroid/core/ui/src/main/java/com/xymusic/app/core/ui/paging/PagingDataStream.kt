package com.xymusic.app.core.ui.paging

import androidx.paging.PagingData
import androidx.paging.map
import com.xymusic.app.domain.paging.PagedStream
import java.util.concurrent.Executor
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.asExecutor
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map

/**
 * Platform-backed [PagedStream] handle.
 *
 * Feature data sources wrap their paging flows with
 * `com.xymusic.app.core.paging.asPagedStream`; presentation adapts them back with
 * [pagingDataFlow].
 */
class PagingDataStream<T : Any>(val flow: Flow<PagingData<T>>) : PagedStream<T>

/**
 * Explicit adapter from the domain [PagedStream] handle to the platform paging flow.
 *
 * Streams that are not backed by [PagingDataStream] adapt to an empty page instead
 * of failing at runtime.
 */
fun <T : Any> PagedStream<T>.pagingDataFlow(): Flow<PagingData<T>> = when (this) {
    is PagingDataStream -> flow
    else -> flowOf(PagingData.empty<T>())
}

/**
 * Maps page items off the main thread.
 *
 * Paging delivers page events on the main dispatcher and the UI mappers build several
 * collections per item, so a plain `map` does that work on the frame that is about to
 * draw the newly loaded page.
 */
fun <T : Any, R : Any> Flow<PagingData<T>>.mapPagedItems(
    executor: Executor = pagingTransformExecutor,
    transform: (T) -> R,
): Flow<PagingData<R>> = map { pagingData ->
    pagingData.map(executor, transform)
}

private val pagingTransformExecutor: Executor = Dispatchers.Default.asExecutor()
