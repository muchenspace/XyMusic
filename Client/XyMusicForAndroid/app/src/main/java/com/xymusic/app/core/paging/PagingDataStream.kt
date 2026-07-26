package com.xymusic.app.core.paging

import androidx.paging.PagingData
import com.xymusic.app.domain.paging.PagedStream
import kotlinx.coroutines.flow.Flow

class PagingDataStream<T : Any>(val flow: Flow<PagingData<T>>) : PagedStream<T>

fun <T : Any> Flow<PagingData<T>>.asPagedStream(): PagedStream<T> = PagingDataStream(this)

fun <T : Any> PagedStream<T>.pagingDataFlow(): Flow<PagingData<T>> = when (this) {
    is PagingDataStream -> flow
    else -> error("Unsupported PagedStream implementation: ${this::class.qualifiedName}")
}
