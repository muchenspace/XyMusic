package com.xymusic.app.core.paging

import androidx.paging.PagingData
import com.xymusic.app.core.ui.paging.PagingDataStream
import com.xymusic.app.domain.paging.PagedStream
import kotlinx.coroutines.flow.Flow

/**
 * Wraps a platform paging flow as the domain [PagedStream] handle.
 *
 * Used by feature data sources; presentation adapts the handle back through
 * `com.xymusic.app.core.ui.paging.pagingDataFlow`.
 */
fun <T : Any> Flow<PagingData<T>>.asPagedStream(): PagedStream<T> = PagingDataStream(this)
