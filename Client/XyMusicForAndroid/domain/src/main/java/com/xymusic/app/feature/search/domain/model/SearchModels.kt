package com.xymusic.app.feature.search.domain.model

import com.xymusic.app.core.model.media.Album
import com.xymusic.app.core.model.media.Artist
import com.xymusic.app.core.model.media.Track
import java.text.Normalizer
import java.util.Locale

@ConsistentCopyVisibility
data class SearchQuery private constructor(val value: String, val normalizedValue: String) {
    companion object {
        fun from(rawValue: String): SearchQuery {
            val normalized = rawValue.trim().replace(WHITESPACE, " ")
            require(normalized.isNotEmpty()) { "Search query cannot be blank" }
            require(normalized.length <= MAX_QUERY_LENGTH) { "Search query is too long" }
            val normalizedForIdentity =
                Normalizer
                    .normalize(normalized, Normalizer.Form.NFKC)
                    .lowercase(Locale.ROOT)
            return SearchQuery(normalized, normalizedForIdentity)
        }

        private val WHITESPACE = Regex("\\s+")
        private const val MAX_QUERY_LENGTH = 200
    }
}

data class SearchOverview(
    val query: SearchQuery,
    val tracks: List<Track>,
    val artists: List<Artist>,
    val albums: List<Album>,
)

enum class SearchScope {
    ALL,
    TRACKS,
    ARTISTS,
    ALBUMS,
}

data class SearchHistoryItem(val query: SearchQuery, val scope: SearchScope, val searchedAtEpochMillis: Long)
