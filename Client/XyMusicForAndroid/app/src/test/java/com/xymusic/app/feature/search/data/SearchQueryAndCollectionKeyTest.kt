package com.xymusic.app.feature.search.data

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.search.domain.model.SearchQuery
import org.junit.Test

class SearchQueryAndCollectionKeyTest {
    @Test
    fun queryCollapsesWhitespaceAndBuildsCaseInsensitiveNfkcIdentity() {
        val first = SearchQuery.from("  Music   TEST ")
        val second = SearchQuery.from("Ｍｕｓｉｃ test")

        assertThat(first.value).isEqualTo("Music TEST")
        assertThat(first.normalizedValue).isEqualTo("music test")
        assertThat(second.normalizedValue).isEqualTo(first.normalizedValue)
        assertThat(second.searchCollectionKey()).isEqualTo(first.searchCollectionKey())
    }

    @Test
    fun collectionKeyContainsOnlyVersionPrefixAndSha256Digest() {
        val query = SearchQuery.from("private search text")
        val key = query.searchCollectionKey()

        assertThat(key).startsWith("search:v1:")
        assertThat(key.removePrefix("search:v1:")).matches("^[a-f0-9]{64}$")
        assertThat(key).doesNotContain(query.value)
    }

    @Test(expected = IllegalArgumentException::class)
    fun blankQueryIsRejected() {
        SearchQuery.from("  \n\t ")
    }
}
