package com.xymusic.app.core.model.search

enum class SearchScope {
    ALL,
    TRACKS,
    ARTISTS,
    ALBUMS,
    ;

    companion object {
        fun fromWireValue(value: String): SearchScope = when (value) {
            "ALL" -> ALL
            "TRACKS" -> TRACKS
            "ARTISTS" -> ARTISTS
            "ALBUMS" -> ALBUMS
            else -> throw IllegalArgumentException("Unknown search scope: $value")
        }
    }
}
