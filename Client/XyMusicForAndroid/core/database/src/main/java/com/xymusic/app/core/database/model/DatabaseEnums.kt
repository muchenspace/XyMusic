package com.xymusic.app.core.database.model

enum class ArtistCreditRole {
    PRIMARY,
    FEATURED,
    COMPOSER,
    LYRICIST,
    PRODUCER,
}

enum class LyricsFormat {
    LRC,
    PLAIN,
}

enum class PlaylistVisibility {
    PRIVATE,
    UNLISTED,
    PUBLIC,
}

enum class SearchScope {
    ALL,
    TRACKS,
    ARTISTS,
    ALBUMS,
}

enum class CatalogItemType {
    TRACK,
    ARTIST,
    ALBUM,
}

enum class SyncOperationType {
    ADD_FAVORITE,
    REMOVE_FAVORITE,
    RECORD_PLAYBACK,
    CREATE_PLAYLIST,
    UPDATE_PLAYLIST,
    DELETE_PLAYLIST,
    ADD_PLAYLIST_ENTRY,
    REMOVE_PLAYLIST_ENTRY,
    REORDER_PLAYLIST_ENTRIES,
}

enum class SyncTargetType {
    FAVORITE,
    PLAYBACK_HISTORY,
    PLAYLIST,
    PLAYLIST_ENTRY,
}

enum class SyncOperationStatus {
    PENDING,
    RUNNING,
    FAILED,
    CONFLICT,
}
