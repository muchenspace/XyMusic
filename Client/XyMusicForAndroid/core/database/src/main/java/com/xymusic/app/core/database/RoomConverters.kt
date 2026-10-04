package com.xymusic.app.core.database

import androidx.room.TypeConverter
import com.xymusic.app.core.database.model.ArtistCreditRole
import com.xymusic.app.core.database.model.CatalogItemType
import com.xymusic.app.core.database.model.SyncOperationStatus
import com.xymusic.app.core.database.model.SyncOperationType
import com.xymusic.app.core.database.model.SyncTargetType
import com.xymusic.app.core.model.media.LyricsFormat
import com.xymusic.app.core.model.media.LyricsTiming
import com.xymusic.app.core.model.playlist.PlaylistVisibility
import com.xymusic.app.core.model.search.SearchScope

class RoomConverters {
    @TypeConverter
    fun artistCreditRoleToString(value: ArtistCreditRole): String = value.name

    @TypeConverter
    fun stringToArtistCreditRole(value: String): ArtistCreditRole = ArtistCreditRole.valueOf(value)

    @TypeConverter
    fun lyricsFormatToString(value: LyricsFormat): String = value.name

    @TypeConverter
    fun stringToLyricsFormat(value: String): LyricsFormat = LyricsFormat.fromWireValue(value)

    @TypeConverter
    fun lyricsTimingToString(value: LyricsTiming): String = value.name

    @TypeConverter
    fun stringToLyricsTiming(value: String): LyricsTiming = LyricsTiming.fromWireValue(value)

    @TypeConverter
    fun playlistVisibilityToString(value: PlaylistVisibility): String = value.name

    @TypeConverter
    fun stringToPlaylistVisibility(value: String): PlaylistVisibility = PlaylistVisibility.fromWireValue(value)

    @TypeConverter
    fun searchScopeToString(value: SearchScope): String = value.name

    @TypeConverter
    fun stringToSearchScope(value: String): SearchScope = SearchScope.fromWireValue(value)

    @TypeConverter
    fun catalogItemTypeToString(value: CatalogItemType): String = value.name

    @TypeConverter
    fun stringToCatalogItemType(value: String): CatalogItemType = CatalogItemType.valueOf(value)

    @TypeConverter
    fun syncOperationTypeToString(value: SyncOperationType): String = value.name

    @TypeConverter
    fun stringToSyncOperationType(value: String): SyncOperationType = SyncOperationType.valueOf(value)

    @TypeConverter
    fun syncTargetTypeToString(value: SyncTargetType): String = value.name

    @TypeConverter
    fun stringToSyncTargetType(value: String): SyncTargetType = SyncTargetType.valueOf(value)

    @TypeConverter
    fun syncOperationStatusToString(value: SyncOperationStatus): String = value.name

    @TypeConverter
    fun stringToSyncOperationStatus(value: String): SyncOperationStatus = SyncOperationStatus.valueOf(value)
}
