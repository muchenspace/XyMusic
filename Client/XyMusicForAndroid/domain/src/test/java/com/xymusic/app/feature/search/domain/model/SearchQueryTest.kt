package com.xymusic.app.feature.search.domain.model

import com.google.common.truth.Truth.assertThat
import org.junit.Assert.assertThrows
import org.junit.Test

class SearchQueryTest {
    @Test
    fun collapsesWhitespaceAndKeepsDisplayCasing() {
        val query = SearchQuery.from("  Hello   World  ")

        assertThat(query.value).isEqualTo("Hello World")
        assertThat(query.normalizedValue).isEqualTo("hello world")
    }

    @Test
    fun blankQueryIsRejected() {
        assertThrows(IllegalArgumentException::class.java) { SearchQuery.from("   ") }
    }

    @Test
    fun overlongQueryIsRejected() {
        assertThrows(IllegalArgumentException::class.java) { SearchQuery.from("a".repeat(201)) }
    }

    @Test
    fun normalizedValueUsesNfkcFolding() {
        val fullWidth = SearchQuery.from("ＡＢＣ")

        assertThat(fullWidth.normalizedValue).isEqualTo("abc")
    }
}
