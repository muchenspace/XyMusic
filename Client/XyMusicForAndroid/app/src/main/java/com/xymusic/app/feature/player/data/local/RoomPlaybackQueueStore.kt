package com.xymusic.app.feature.player.data.local

import com.xymusic.app.core.common.DefaultDispatcher
import com.xymusic.app.core.database.dao.PlaybackQueueDao
import com.xymusic.app.core.database.entity.PlaybackQueueEntity
import com.xymusic.app.core.session.AppSessionProvider
import com.xymusic.app.core.session.AppSessionState
import com.xymusic.app.core.session.SessionMutationCoordinator
import com.xymusic.app.feature.player.domain.PlaybackQueueStore
import com.xymusic.app.feature.player.domain.PlayerResult
import com.xymusic.app.feature.player.domain.StoredPlaybackQueueItem
import com.xymusic.app.feature.player.domain.model.PlayerFailure
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.withContext
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

@Singleton
class RoomPlaybackQueueStore
@Inject
constructor(
    private val playbackQueueDao: PlaybackQueueDao,
    private val appSessionProvider: AppSessionProvider,
    private val sessionMutationCoordinator: SessionMutationCoordinator,
    private val json: Json,
    @DefaultDispatcher private val defaultDispatcher: CoroutineDispatcher,
) : PlaybackQueueStore {
    @OptIn(ExperimentalCoroutinesApi::class)
    override fun observe(): Flow<List<StoredPlaybackQueueItem>> =
        appSessionProvider.sessionState.flatMapLatest { session ->
            when (session) {
                is AppSessionState.SignedIn ->
                    playbackQueueDao
                        .observe(session.userId)
                        .map { entities ->
                            withContext(defaultDispatcher) { entities.map(::toDomain) }
                        }.catch { failure ->
                            if (failure is CancellationException) throw failure
                            emit(emptyList())
                        }
                AppSessionState.Loading,
                AppSessionState.SignedOut,
                -> flowOf(emptyList())
            }
        }

    override suspend fun replace(ownerUserId: String, items: List<StoredPlaybackQueueItem>): PlayerResult<Unit> =
        withSignedInOwner(ownerUserId) { activeOwnerUserId ->
            val entities =
                withContext(defaultDispatcher) {
                    items.map { item -> item.toEntity(activeOwnerUserId, json) }
                }
            playbackQueueDao.replace(
                ownerUserId = activeOwnerUserId,
                items = entities,
            )
        }

    override suspend fun updatePosition(
        ownerUserId: String,
        queueItemId: String,
        positionMs: Long,
    ): PlayerResult<Unit> = withSignedInOwner(ownerUserId) { activeOwnerUserId ->
        playbackQueueDao.updateResumePosition(activeOwnerUserId, queueItemId, positionMs)
    }

    override suspend fun setCurrent(ownerUserId: String, queueItemId: String, positionMs: Long): PlayerResult<Unit> =
        withSignedInOwner(ownerUserId) { activeOwnerUserId ->
            playbackQueueDao.setCurrent(activeOwnerUserId, queueItemId, positionMs)
        }

    override suspend fun clear(ownerUserId: String): PlayerResult<Unit> =
        withSignedInOwner(ownerUserId) { activeOwnerUserId ->
            playbackQueueDao.clear(activeOwnerUserId)
        }

    private suspend fun withSignedInOwner(
        expectedOwnerUserId: String,
        action: suspend (ownerUserId: String) -> Unit,
    ): PlayerResult<Unit> {
        if (expectedOwnerUserId.isBlank()) return PlayerResult.Failure(PlayerFailure.InvalidQueue)
        return try {
            sessionMutationCoordinator.mutate {
                val activeOwnerUserId =
                    (
                        appSessionProvider.sessionState.value as? AppSessionState.SignedIn
                        )?.userId ?: return@mutate PlayerResult.Failure(
                        PlayerFailure.Unexpected("Playback queue requires an authenticated session"),
                    )
                if (activeOwnerUserId != expectedOwnerUserId) {
                    return@mutate PlayerResult.Failure(
                        PlayerFailure.Unexpected("Playback queue owner changed before persistence"),
                    )
                }
                action(activeOwnerUserId)
                PlayerResult.Success(Unit)
            }
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: IllegalArgumentException) {
            PlayerResult.Failure(PlayerFailure.InvalidQueue)
        } catch (failure: Exception) {
            PlayerResult.Failure(PlayerFailure.Unexpected(failure.message))
        }
    }

    private fun toDomain(entity: PlaybackQueueEntity): StoredPlaybackQueueItem = StoredPlaybackQueueItem(
        queueItemId = entity.itemId,
        position = entity.position,
        trackId = entity.trackId,
        variantId = entity.variantId,
        stableCacheKey = entity.stableCacheKey,
        resumePositionMs = entity.resumePositionMs,
        isCurrent = entity.isCurrent,
        enqueuedAtEpochMillis = entity.enqueuedAtEpochMs,
        title = entity.title.ifBlank { entity.trackId },
        artistNames =
        runCatching {
            json.decodeFromString<List<String>>(entity.artistNamesJson)
        }.getOrDefault(emptyList()),
        albumTitle = entity.albumTitle,
        artworkUrl = entity.artworkUrl,
        artworkCacheKey = entity.artworkCacheKey,
        durationMs = entity.durationMs,
    )
}

private fun StoredPlaybackQueueItem.toEntity(ownerUserId: String, json: Json): PlaybackQueueEntity =
    PlaybackQueueEntity(
        ownerUserId = ownerUserId,
        itemId = queueItemId,
        position = position,
        trackId = trackId,
        variantId = variantId,
        stableCacheKey = stableCacheKey,
        resumePositionMs = resumePositionMs,
        isCurrent = isCurrent,
        enqueuedAtEpochMs = enqueuedAtEpochMillis,
        title = title,
        artistNamesJson = json.encodeToString(artistNames),
        albumTitle = albumTitle,
        artworkUrl = artworkUrl,
        artworkCacheKey = artworkCacheKey,
        durationMs = durationMs,
    )
