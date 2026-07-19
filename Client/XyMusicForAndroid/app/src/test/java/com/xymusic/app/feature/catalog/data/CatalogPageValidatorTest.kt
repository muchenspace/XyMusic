package com.xymusic.app.feature.catalog.data

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.data.media.remote.CatalogProtocolException
import com.xymusic.app.core.data.media.remote.RemotePage
import com.xymusic.app.core.data.media.validateCatalogPage
import org.junit.Test

class CatalogPageValidatorTest {
    @Test
    fun acceptsUniqueItemsAndAnAdvancingCursor() {
        validateCatalogPage(
            page = RemotePage(listOf("a", "b"), "cursor-2"),
            requestedCursor = "cursor-1",
            itemId = { it },
        )
    }

    @Test
    fun duplicateIdsAreProtocolFailures() {
        val failure =
            runCatching {
                validateCatalogPage(
                    page = RemotePage(listOf("a", "a"), null),
                    requestedCursor = null,
                    itemId = { it },
                )
            }.exceptionOrNull()

        assertThat(failure).isInstanceOf(CatalogProtocolException::class.java)
    }

    @Test
    fun emptyPageCannotAdvertiseAnotherCursor() {
        val failure =
            runCatching {
                validateCatalogPage(
                    page = RemotePage(emptyList<String>(), "cursor-2"),
                    requestedCursor = "cursor-1",
                    itemId = { it },
                )
            }.exceptionOrNull()

        assertThat(failure).isInstanceOf(CatalogProtocolException::class.java)
    }

    @Test
    fun nextCursorMustAdvance() {
        val failure =
            runCatching {
                validateCatalogPage(
                    page = RemotePage(listOf("a"), "cursor-1"),
                    requestedCursor = "cursor-1",
                    itemId = { it },
                )
            }.exceptionOrNull()

        assertThat(failure).isInstanceOf(CatalogProtocolException::class.java)
    }
}
