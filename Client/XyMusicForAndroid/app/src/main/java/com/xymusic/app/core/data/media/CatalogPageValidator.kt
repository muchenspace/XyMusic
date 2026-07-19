package com.xymusic.app.core.data.media

import com.xymusic.app.core.data.media.remote.CatalogProtocolException
import com.xymusic.app.core.data.media.remote.RemotePage

internal fun <T> validateCatalogPage(page: RemotePage<T>, requestedCursor: String?, itemId: (T) -> String) {
    val itemIds = page.items.map(itemId)
    if (itemIds.distinct().size != itemIds.size) {
        throw CatalogProtocolException("Catalog page contains duplicate item IDs")
    }
    if (page.items.isEmpty() && page.nextCursor != null) {
        throw CatalogProtocolException("Empty catalog page cannot have a next cursor")
    }
    if (page.nextCursor != null && page.nextCursor == requestedCursor) {
        throw CatalogProtocolException("Catalog next cursor did not advance")
    }
}
