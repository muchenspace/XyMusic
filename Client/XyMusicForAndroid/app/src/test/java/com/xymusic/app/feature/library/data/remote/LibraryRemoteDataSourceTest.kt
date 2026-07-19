package com.xymusic.app.feature.library.data.remote

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.data.media.remote.ArtistReferenceDto
import com.xymusic.app.core.data.media.remote.TrackSummaryDto
import com.xymusic.app.core.network.ProblemMapper
import com.xymusic.app.data.network.ProblemResponseParser
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import org.junit.Test
import retrofit2.Response

class LibraryRemoteDataSourceTest {
    @Test
    fun historyPagesAreConsumedBeforeTheFollowingPageIsRequested() = runTest {
        val api =
            RecordingLibraryApi(
                mapOf(
                    null to HistoryPageDto(listOf(historyItem("first")), "next"),
                    "next" to HistoryPageDto(listOf(historyItem("second")), null),
                ),
            )
        val remote =
            HttpLibraryRemoteDataSource(
                api = api,
                problemResponseParser = ProblemResponseParser(Json, ProblemMapper()),
            )
        val receivedTrackIds = mutableListOf<String>()

        remote.historyPages().collect { page ->
            receivedTrackIds += page.map { it.track.id }
            if (receivedTrackIds.size == 1) {
                assertThat(api.requestedHistoryCursors).hasSize(1)
                assertThat(api.requestedHistoryCursors.single()).isNull()
            }
        }

        assertThat(receivedTrackIds).containsExactly("first", "second").inOrder()
        assertThat(api.requestedHistoryCursors).containsExactly(null, "next").inOrder()
    }

    private class RecordingLibraryApi(private val historyPages: Map<String?, HistoryPageDto>) : LibraryApi {
        val requestedHistoryCursors = mutableListOf<String?>()

        override suspend fun favorites(cursor: String?, limit: Int, sort: String): Response<FavoritePageDto> =
            error("unused")

        override suspend fun addFavorite(trackId: String): Response<FavoriteItemDto> = error("unused")

        override suspend fun removeFavorite(trackId: String): Response<Unit> = error("unused")

        override suspend fun history(cursor: String?, limit: Int): Response<HistoryPageDto> {
            requestedHistoryCursors += cursor
            return Response.success(requireNotNull(historyPages[cursor]))
        }

        override suspend fun recordPlayback(
            trackId: String,
            idempotencyKey: String,
            request: RecordPlaybackRequestDto,
        ): Response<HistoryItemDto> = error("unused")
    }

    private companion object {
        fun historyItem(trackId: String) = HistoryItemDto(
            track =
            TrackSummaryDto(
                id = trackId,
                title = "Track $trackId",
                artists = listOf(ArtistReferenceDto("artist", "Artist")),
                album = null,
                artwork = null,
                durationMs = 180_000,
                trackNumber = 1,
                discNumber = 1,
                isFavorite = false,
                publishedAt = "2026-07-10T00:00:00Z",
            ),
            lastPositionMs = 1_000,
            playCount = 1,
            lastPlayedAt = "2026-07-11T00:00:00Z",
            completed = false,
            updatedAt = "2026-07-11T00:00:00Z",
        )
    }
}
