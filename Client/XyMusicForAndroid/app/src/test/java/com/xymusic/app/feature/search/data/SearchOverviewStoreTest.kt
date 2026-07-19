package com.xymusic.app.feature.search.data

import com.google.common.truth.Truth.assertThat
import org.junit.Test

class SearchOverviewStoreTest {
    @Test
    fun recentOverviewKeysStayBoundedAndKeepNewestKey() {
        val result =
            rememberRecentKey(
                keys = linkedSetOf("first", "second", "third"),
                key = "fourth",
                maximumSize = 3,
            )

        assertThat(result).containsExactly("second", "third", "fourth").inOrder()
    }

    @Test
    fun existingKeyDoesNotAllocateANewSet() {
        val keys = setOf("query")

        assertThat(rememberRecentKey(keys, "query")).isSameInstanceAs(keys)
    }
}
